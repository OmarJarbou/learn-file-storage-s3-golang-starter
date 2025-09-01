package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
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
