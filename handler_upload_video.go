package main

import (
	"io"
	"log"
	"mime"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid video ID for upload", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "JWT not received", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Invalid JWT", err)
		return
	}

	const maxMemory = 2 << 30 //1GB partition for video uploads
	err = r.ParseMultipartForm(int64(maxMemory))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to parse HTTP request data", err)
		return
	}
	file, fileHeader, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to extract file from request", err)
		return
	}
	defer file.Close()

	contentType := fileHeader.Header.Get("Content-Type")
	if contentType == "" {
		respondWithError(w, http.StatusBadRequest, "Missing Content-Type header", nil)
		return
	}

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to find video with given ID", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized for given video", nil)
		return
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to parse media type", err)
		return
	}
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusUnsupportedMediaType, "Invalid video file type", nil)
		return
	}
	fileExtension := "mp4"

	fileName := "tubely-upload." + fileExtension
	tempFile, err := os.CreateTemp("", fileName)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create temp file", err)
		return
	}
	defer os.Remove(fileName)
	defer tempFile.Close()

	_, err = io.Copy(tempFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to copy file data", err)
		return
	}

	outputFilePath, err := processVideosForFastStart(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error processing video for fast start", err)
		return
	}
	outputFile, err := os.Open(outputFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to open output file", err)
		return
	}
	defer os.Remove(outputFilePath)
	defer outputFile.Close()

	aspectRation, err := getVideoAspectRatio(outputFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to get video aspect ratio", err)
		return
	}

	var videoOrientation string
	switch aspectRation {
	case "16:9":
		videoOrientation = "landscape"
	case "9:16":
		videoOrientation = "portrait"
	case "other":
		videoOrientation = "other"
	default:
		respondWithError(w, http.StatusInternalServerError, "invalid aspect ratio", nil)
	}

	outputFile.Seek(0, io.SeekStart)

	awsKey := videoOrientation + "/" + makeRandomFileName() + "." + fileExtension
	bucketName := cfg.s3Bucket

	_, err = cfg.s3Client.PutObject(r.Context(),
		&s3.PutObjectInput{
			Bucket:      &bucketName,
			Key:         &awsKey,
			Body:        outputFile,
			ContentType: &mediaType,
		})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to put video in s3", err)
		return
	}

	url := cfg.s3CfDistribution + "/" + awsKey
	log.Printf("uploaded video url: %s", url)
	video.VideoURL = &url
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to update video data", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}
