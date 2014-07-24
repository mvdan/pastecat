/* Copyright (c) 2014, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	chars   = "abcdefghijklmnopqrstuvwxyz0123456789"
	idSize  = 8
	siteUrl = "http://localhost:9090"
	listen  = "localhost:9090"
	dataDir = "data"
	maxSize = 1 << 20
	minLife = 1 * time.Minute
	defLife = 1 * time.Hour
	maxLife = 72 * time.Hour

	// GET error messages
	invalidId     = "Invalid paste id."
	pasteNotFound = "Paste doesn't exist."
	unknownError  = "Something went wrong. Woop woop woop woop!"
	// POST error messages
	missingForm = "Form with paste could not be found."
)

var validId *regexp.Regexp = regexp.MustCompile("^[a-z0-9]{" + strconv.FormatInt(idSize, 10) + "}$")

func pathId(id string) string {
	return path.Join(id[0:2], id[2:4], id[4:8])
}

func randomId() string {
	s := make([]byte, idSize)
	var offset uint = 0
	for {
		r := rand.Int63()
		for i := 0; i < 8; i++ {
			randbyte := int(r&0xff) % len(chars)
			s[offset] = chars[randbyte]
			offset++
			if offset == idSize {
				return string(s)
			}
			r >>= 8
		}
	}
	return strings.Repeat(chars[0:1], idSize)
}

func endLife(pastePath string) {
	err := os.Remove(pastePath)
	if err != nil {
		log.Printf("Could not end the life of %s: %s", pastePath, err)
		timer := time.NewTimer(minLife)
		go func() {
			<-timer.C
			endLife(pastePath)
		}()
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		id := r.URL.Path[1:]
		if len(id) == 0 {
			fmt.Fprintf(w, "<html><body><form action=\"%s\" method=\"post\" enctype=\"multipart/form-data\"><textarea cols=80 rows=48 name=\"paste\"></textarea><br><button type=\"submit\">paste</button></form></body></html>", siteUrl)
			return
		}
		if !validId.MatchString(id) {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "%s\n", invalidId)
			return
		}
		pastePath := pathId(id)
		pasteFile, err := os.Open(pastePath)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, "%s\n", pasteNotFound)
			return
		}
		compReader, err := gzip.NewReader(pasteFile)
		if err != nil {
			log.Printf("Could not open a compression reader for %s: %s", pastePath, err)
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "%s\n", unknownError)
			return
		}
		io.Copy(w, compReader)
		compReader.Close()
		pasteFile.Close()

	case "POST":
		r.Body = http.MaxBytesReader(w, r.Body, maxSize)
		var id, pastePath string
		for {
			id = randomId()
			pastePath = pathId(id)
			if _, err := os.Stat(pastePath); os.IsNotExist(err) {
				break
			}
		}
		err := r.ParseMultipartForm(maxSize << 1)
		if err != nil {
			log.Printf("Could not parse POST multipart form: %s", err)
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "%s\n", unknownError)
			return
		}
		var life time.Duration
		vs, found := r.Form["life"]
		if !found {
			life = defLife
		} else {
			life, err = time.ParseDuration(vs[0])
		}
		if life < minLife {
			life = minLife
		} else if life > maxLife {
			life = maxLife
		}
		vs, found = r.Form["paste"]
		if !found {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "%s\n", missingForm)
			return
		}
		dir, _ := path.Split(pastePath)
		err = os.MkdirAll(dir, 0700)
		if err != nil {
			log.Printf("Could not create directories leading to %s: %s", pastePath, err)
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "%s\n", unknownError)
			return
		}
		timer := time.NewTimer(life)
		go func() {
			<-timer.C
			endLife(pastePath)
		}()
		pasteFile, err := os.OpenFile(pastePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			log.Printf("Could not create new paste pasteFile %s: %s", pastePath, err)
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "%s\n", unknownError)
			return
		}
		compWriter := gzip.NewWriter(pasteFile)
		_, err = io.WriteString(compWriter, vs[0])
		compWriter.Close()
		pasteFile.Close()
		if err != nil {
			log.Printf("Could not write compressed data into %s: %s", pastePath, err)
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "%s\n", unknownError)
			return
		}
		fmt.Fprintf(w, "%s/%s\n", siteUrl, id)
	}
}

func main() {
	os.Mkdir(dataDir, 0700)
	err := os.Chdir(dataDir)
	if err != nil {
		log.Printf("Could not enter data directory %s: %s", dataDir, err)
		return
	}
	http.HandleFunc("/", handler)
	http.ListenAndServe(listen, nil)
}
