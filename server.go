// Copyright 2014 Julien Schmidt. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found
// in the LICENSE file.

// Package main provides the server application handling the server-sent-events
// for the remote photo show.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/julienschmidt/httprouter"
	"github.com/julienschmidt/sse"
)

// Set your config here
const (
	host     string = ":8080"
	photoDir string = "./photos/"

	// HTTPS config
	https   bool   = false
	crtPath string = "/etc/ssl/http.pem"
	keyPath string = "/etc/ssl/http.key"

	// Credentials for master site
	username string = "gordon"
	password string = "secret!"
)

var (
	streamer  *sse.Streamer
	imgID     uint64
	endID     uint64
	photoJSON []byte
	photoErr  error
)

// BasicAuth is a httprouter.Handle wrapper for Basic HTTP Authentication
func BasicAuth(h httprouter.Handle, user, pass []byte) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		const basicAuthPrefix string = "Basic "

		// Get the Basic Authentication credentials
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, basicAuthPrefix) {
			// Check credentials
			payload, err := base64.StdEncoding.DecodeString(auth[len(basicAuthPrefix):])
			if err == nil {
				pair := bytes.SplitN(payload, []byte(":"), 2)
				if len(pair) == 2 && bytes.Equal(pair[0], user) && bytes.Equal(pair[1], pass) {
					// Delegate request to the given handle
					h(w, r, ps)
					return
				}
			}
		}

		// Request Basic Authentication otherwise
		w.Header().Set("WWW-Authenticate", "Basic realm=Restricted")
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
	}
}

// reset reloads the photos and restarts the photo show
func reset() {
	imgID = 0
	photoJSON, photoErr = loadPhotos()
	streamer.SendString("", "reset", "")
}

// setID sets the current photo show image ID and sends notifications to all clients
func setID(id uint64) error {
	if id > endID {
		return errors.New("invalid ID")
	}

	imgID = id
	streamer.SendUint("", "set", id)

	return nil
}

// loadPhotos gets all files in the photo dir and saves them as a list in JSON
func loadPhotos() ([]byte, error) {
	dir, err := os.Open(photoDir)
	if err != nil {
		return nil, err
	}
	defer dir.Close()

	fi, err := dir.Stat()
	if err != nil {
		return nil, err
	}

	filenames := make([]string, 0)
	if fi.IsDir() {
		fis, err := dir.Readdir(-1) // -1 means return all the FileInfos
		if err != nil {
			return nil, err
		}

		for _, fileinfo := range fis {
			if !fileinfo.IsDir() {
				filenames = append(filenames, fileinfo.Name())
			}
		}
	}

	endID = uint64(len(filenames)) - 1
	return json.Marshal(filenames)
}

func PhotoShow(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	http.ServeFile(w, r, "remotephoto.html")
}

func PhotoMaster(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	http.ServeFile(w, r, "remotemaster.html")
}

func PhotoMasterCMD(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	switch r.PostFormValue("cmd") {
	case "set":
		id, err := strconv.ParseUint(r.PostFormValue("id"), 10, 0)

		if err == nil {
			err = setID(uint64(id))
		}

		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return

	case "reset":
		reset()
		return

	default:
		http.Error(w, "Invalid CMD", http.StatusInternalServerError)
		return
	}
}

func PhotosJSON(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if photoErr != nil {
		http.Error(w, photoErr.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprintf(w, `{"photos": %s, "id": %d}`, photoJSON, imgID)
}

func PhotosServer(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	http.ServeFile(w, r, photoDir+ps.ByName("photo"))
}

func Favicon(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	http.ServeFile(w, r, "favicon.ico")
}

func main() {
	user := []byte(username)
	pass := []byte(password)

	router := httprouter.New()
	router.GET("/", PhotoShow)
	router.GET("/master", BasicAuth(PhotoMaster, user, pass))
	router.POST("/master", BasicAuth(PhotoMasterCMD, user, pass))
	router.GET("/photos.json", PhotosJSON)
	router.GET("/photos/:photo", PhotosServer)
	// router.GET("/favicon.ico", Favicon)

	// Server-Sent Events
	streamer = sse.New()
	router.Handler("GET", "/listen", streamer)

	// Initialize photo show
	reset()

	if https {
		log.Fatal("HTTPS server error: ", http.ListenAndServeTLS(host, crtPath, keyPath, router))
	} else {
		log.Fatal("HTTP server error: ", http.ListenAndServe(host, router))
	}
}
