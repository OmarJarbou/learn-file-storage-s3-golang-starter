package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	maxUploadLimit := int64(1 << 30)
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadLimit)
	if err := r.ParseMultipartForm(maxUploadLimit); err != nil {
		respondWithError(w, http.StatusBadRequest, "File too big or invalid form", err)
		return
	}

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	fmt.Println("uploading video", videoID, "by user", userID)

	vedioMetadata, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error fetching vedio with this id", err)
		return
	}
	if vedioMetadata.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "You are not the owner of this video!", err)
		return
	}

	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	mediaType := header.Header.Get("Content-Type")
	mimeType, _, err := mime.ParseMediaType(mediaType)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error while parsing media type to mime", err)
		return
	}
	if !(mimeType == "video/mp4") {
		mimeErr := errors.New("error while processing mime type")
		respondWithError(w, http.StatusBadRequest, "Only files of type image/jpeg and image/png can be uploaded", mimeErr)
		return
	}

	mediaTypeParts := strings.Split(mimeType, "/")
	fileExtention := mediaTypeParts[len(mediaTypeParts)-1]
	videoFileID := make([]byte, 32)
	rand.Read(videoFileID)
	videoFileIDString := base64.RawURLEncoding.EncodeToString(videoFileID)
	fileName := videoFileIDString + "." + fileExtention
	createdFile, err := os.CreateTemp("", fileName)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error while creating the file", err)
		return
	}
	defer os.Remove(fileName)
	defer createdFile.Close()
	_, err = io.Copy(createdFile, file)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error while copying multipart file contnet to the new file", err)
		return
	}

	_, err = createdFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error while adjusting offset in the file", err)
		return
	}

	ratio, err := getVideoAspectRatio(createdFile.Name())
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error while calculating aspect ratio of the file: "+err.Error(), err)
		return
	}
	if ratio == "16:9" {
		fileName = "landscape/" + fileName
	} else if ratio == "9:16" {
		fileName = "portrait/" + fileName
	} else {
		fileName = "other/" + fileName
	}

	putObjectInput := s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &fileName,
		Body:        createdFile,
		ContentType: &mimeType,
	}
	_, err = cfg.s3Client.PutObject(r.Context(), &putObjectInput)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error while put video object in amazon servers", err)
		return
	}

	videoURL := "https://" + cfg.s3Bucket + ".s3." + cfg.s3Region + ".amazonaws.com/" + fileName
	vedioMetadata.VideoURL = &videoURL
	err = cfg.db.UpdateVideo(vedioMetadata)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error updating video to contain video url", err)
		return
	}

	respondWithJSON(w, http.StatusOK, vedioMetadata)
}

func classifyAspectRatio(width, height int) string {
	if width <= 0 || height <= 0 {
		return "invalid"
	}

	ratio := float64(width) / float64(height)

	knownRatios := map[string]float64{
		"16:9": 16.0 / 9.0,
		"9:16": 9.0 / 16.0,
	}

	tolerance := 0.02 // 2% tolerance

	closest := "other"
	for label, ref := range knownRatios {
		if math.Abs(ratio-ref) < tolerance {
			closest = label
			break
		}
	}

	return closest
}

func getVideoAspectRatio(filePath string) (string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	var buffer bytes.Buffer
	cmd.Stdout = &buffer
	cmd.Stderr = &buffer

	err := cmd.Run()
	if err != nil {
		returnErr := errors.New("Error while executing ffprobe command: " + err.Error() + "\nOutput: " + buffer.String())
		return "", returnErr
	}

	var data ffprobeOutput
	err = json.Unmarshal(buffer.Bytes(), &data)
	if err != nil {
		returnErr := errors.New("Error decoding json result of ffprobe: " + err.Error())
		return "", returnErr
	}

	width := data.Streams[0].Width
	height := data.Streams[0].Height

	if height == 0 {
		returnErr := errors.New("height cannot be zero")
		return "", returnErr
	}

	aspectRatio := classifyAspectRatio(width, height)

	if aspectRatio == "16:9" || aspectRatio == "9:16" {
		return aspectRatio, nil
	}

	return "other", nil
}
