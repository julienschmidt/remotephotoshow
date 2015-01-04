// Copyright 2014 Julien Schmidt. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found
// in the LICENSE file.
// SSE code based on https://gist.github.com/ismasan/3fb75381cd2deb6bfa9c

// Package main provides the server application handling the server-sent-events
// for the remote photo show.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
)

// Set your config here
const (
	https    bool   = false
	host     string = ":8080"
	crtPath  string = "/etc/ssl/http.pem"
	keyPath  string = "/etc/ssl/http.key"
	photoDir string = "./photos/"
)

var (
	broker    *Broker
	imgID     uint
	endID     uint
	photoJSON []byte
	photoErr  error
)

type Broker struct {
	// Events are pushed to this channel by the main events-gathering routine
	Notifier chan string

	// New client connections
	newClients chan chan string

	// Closed client connections
	closingClients chan chan string

	// Client connections registry
	clients map[chan string]bool
}

func NewServer() (broker *Broker) {
	// Instantiate a broker
	broker = &Broker{
		Notifier:       make(chan string, 1),
		newClients:     make(chan chan string),
		closingClients: make(chan chan string),
		clients:        make(map[chan string]bool),
	}

	// Set it running - listening and broadcasting events
	go broker.listen()

	return
}

func (broker *Broker) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// Make sure that the writer supports flushing.
	flusher, ok := rw.(http.Flusher)
	if !ok {
		http.Error(rw, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-cache")
	rw.Header().Set("Connection", "keep-alive")
	rw.Header().Set("Access-Control-Allow-Origin", "*")

	// Each connection registers its own message channel with the Broker's
	// connections registry
	messageChan := make(chan string)

	// Signal the broker that we have a new connection
	broker.newClients <- messageChan

	// Remove this client from the map of connected clients when this handler
	// exits.
	defer func() {
		broker.closingClients <- messageChan
	}()

	// Listen to connection close and deregister messageChan
	notify := rw.(http.CloseNotifier).CloseNotify()

	go func() {
		<-notify
		broker.closingClients <- messageChan
	}()

	for {
		// Write to the ResponseWriter
		// Server Sent Events compatible
		fmt.Fprintf(rw, "data: %s\n\n", <-messageChan)

		// Flush the data immediately instead of buffering it for later.
		flusher.Flush()
	}
}

func (broker *Broker) listen() {
	for {
		select {
		case s := <-broker.newClients:
			// A new client has connected. Register their message channel
			broker.clients[s] = true
			log.Printf("Client added. %d registered clients", len(broker.clients))

		case s := <-broker.closingClients:
			// A client has disconnected and we want to stop sending messages
			// to this client.
			delete(broker.clients, s)
			log.Printf("Removed client. %d registered clients", len(broker.clients))

		case event := <-broker.Notifier:
			// We got a new event from the outside!
			// Send event to all connected clients
			fmt.Println("Send event:", event)
			for clientMessageChan, _ := range broker.clients {
				clientMessageChan <- event
			}
		}
	}
}

func reset() {
	imgID = 0
	photoJSON, photoErr = loadPhotos()
	broker.Notifier <- "r"
}

func setID(id uint) error {
	if id > endID {
		errors.New("Invalid ID")
	}

	imgID = id
	broker.Notifier <- fmt.Sprintf("s%d", id)

	return nil
}

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

	return json.Marshal(filenames)
}

func handlePhotos(rw http.ResponseWriter, req *http.Request) {
	if photoErr != nil {
		http.Error(rw, photoErr.Error(), http.StatusInternalServerError)
		return
	}

	rw.Header().Set("Content-Type", "application/json")
	rw.Header().Set("Cache-Control", "no-cache")
	fmt.Fprintf(rw, `{"photos": %s, "id": %d}`, photoJSON, imgID)
}

func handleCMD(rw http.ResponseWriter, req *http.Request) {
	switch req.PostFormValue("cmd") {
	case "set":
		id, err := strconv.ParseUint(req.PostFormValue("id"), 10, 0)

		if err == nil {
			err = setID(uint(id))
		}

		if err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
		}
		return

	case "reset":
		reset()
		return

	default:
		http.Error(rw, "Illegal CMD", http.StatusInternalServerError)
		return
	}
}

func main() {
	// SSE client broker
	broker = NewServer()
	http.Handle("/listen", broker)

	reset()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "remotephoto.html")
	})

	http.HandleFunc("/master", func(w http.ResponseWriter, r *http.Request) {
		// TODO: use Basic HTTP auth
		switch r.Method {
		case "GET":
			http.ServeFile(w, r, "remotemaster.html")
		case "POST":
			handleCMD(w, r)
		default:
			http.Error(w, "Method not allowed!", http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/assets/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, r.URL.Path[1:])
	})
	/*http.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
	    http.ServeFile(w, r, "favicon.ico")
	})*/

	http.HandleFunc("/photos.json", handlePhotos)
	http.HandleFunc("/photos/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, photoDir+r.URL.Path[7:])
	})

	if https {
		log.Fatal("HTTPS server error: ", http.ListenAndServeTLS(host, crtPath, keyPath, nil))
	} else {
		log.Fatal("HTTP server error: ", http.ListenAndServe(host, nil))
	}
}
