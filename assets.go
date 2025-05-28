package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (cfg apiConfig) ensureAssetsDir() error {
	if _, err := os.Stat(cfg.assetsRoot); os.IsNotExist(err) {
		return os.Mkdir(cfg.assetsRoot, 0755)
	}
	return nil
}

func parseFileExtension(contentType string) string {
	splitContentType := strings.Split(contentType, "/")
	if len(splitContentType) != 2 {
		return "bin"
	}
	return splitContentType[1]
}

func (cfg apiConfig) getFilePath(fileName string, fileExtension string) string {
	return filepath.Join(cfg.assetsRoot, fileName+"."+fileExtension)
}

func (cfg apiConfig) getVideoURL(fileName string, fileExtension string) string {
	return fmt.Sprintf("http://localhost:%s/assets/%s.%s", cfg.port, fileName, fileExtension)
}

func (cfg apiConfig) getAWSVideoURL(fileName string) string {
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, fileName)
}

func makeRandomFileName() string {
	randomBinary := make([]byte, 32)
	rand.Read(randomBinary)
	return base64.RawURLEncoding.EncodeToString(randomBinary)
}

func getVideoAspectRatio(filepath string) (string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filepath)
	var b bytes.Buffer
	cmd.Stdout = &b

	err := cmd.Run()
	if err != nil {
		return "", err
	}

	var output struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}

	err = json.Unmarshal(b.Bytes(), &output)
	if err != nil {
		return "", err
	}
	if len(output.Streams) == 0 {
		return "", errors.New("no video streams found")
	}

	w := float32(output.Streams[0].Width)
	h := float32(output.Streams[0].Height)
	if w/h*9 > 15.8 && w/h*9 < 16.2 {
		return "16:9", nil
	}
	if h/w*16 > 15.8 && h/w*9 < 16.2 {
		return "9:16", nil
	}
	return "other", nil
}

func processVideosForFastStart(filePath string) (string, error) {
	newFilePath := filePath + ".processing"
	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", newFilePath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", errors.New("error running 'faststart' command")
	}

	fileInfo, err := os.Stat(newFilePath)
	if err != nil {
		return "", fmt.Errorf("could not stat processed file: %v", err)
	}
	if fileInfo.Size() == 0 {
		return "", fmt.Errorf("processed file is empty")
	}

	return newFilePath, nil
}
