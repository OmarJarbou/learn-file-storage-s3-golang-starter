package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
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

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	// TODO: implement the upload here
	vedioMetadata, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error fetching vedio with this id", err)
		return
	}
	if vedioMetadata.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "You are not the owner of this video!", err)
		return
	}

	// Set a const maxMemory to 10MB
	const maxMemory = 10 << 20 // bit-shifted the number 10 to the left 20 times to get an int that stores the proper number of bytes
	//Bit shifting is a way to multiply by powers of 2. 10 << 20 is the same as 10 * 1024 * 1024, which is 10MB.
	r.ParseMultipartForm(maxMemory)

	// "thumbnail" should match the HTML form input name
	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	// The Content-Type is in the header.Header map, not via Get method
	mediaType := header.Header.Get("Content-Type")
	data, err := io.ReadAll(file)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error reading thumbnail file content", err)
		return
	}

	// Update video metadata to contain thumbnail url
	base64ImageData := base64.StdEncoding.EncodeToString(data)
	thumbnailURL := "data:" + mediaType + ";base64," + base64ImageData
	vedioMetadata.ThumbnailURL = &thumbnailURL
	err = cfg.db.UpdateVideo(vedioMetadata)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error updating video to contain thumbnail url", err)
		return
	}

	respondWithJSON(w, http.StatusOK, vedioMetadata)
}
