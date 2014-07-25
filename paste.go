/* Copyright (c) 2014, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"compress/zlib"
	"fmt"
	"html/template"
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
	idSize    = 8 // should be between 6 and 256
	siteUrl   = "http://localhost:9090"
	listen    = "localhost:9090"
	indexTmpl = "index.html"
	dataDir   = "data"
	maxSize   = 1 << 20 // before compression
	lifeTime  = 12 * time.Hour

	// GET error messages
	invalidId     = "Invalid paste id."
	pasteNotFound = "Paste doesn't exist."
	unknownError  = "Something went terribly wrong."
	// POST error messages
	missingForm = "Paste could not be found inside the posted form."
)

const chars = "abcdefghijklmnopqrstuvwxyz0123456789"

var validId *regexp.Regexp = regexp.MustCompile("^[a-z0-9]{" + strconv.FormatInt(idSize, 10) + "}$")

var indexTemplate *template.Template

func pathId(id string) string {
	return path.Join(id[0:2], id[2:4], id[4:])
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

func endLife(id string) {
	pastePath := pathId(id)
	err := os.Remove(pastePath)
	if err == nil {
		log.Printf("Removed paste: %s", id)
	} else {
		log.Printf("Could not end the life of %s: %s", id, err)
		timer := time.NewTimer(2 * time.Minute)
		go func() {
			<-timer.C
			endLife(id)
		}()
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	var err error
	switch r.Method {
	case "GET":
		id := r.URL.Path[1:]
		if len(id) == 0 {
			indexTemplate.Execute(w, siteUrl)
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
		compReader, err := zlib.NewReader(pasteFile)
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
		if err = r.ParseMultipartForm(maxSize << 1); err != nil {
			log.Printf("Could not parse POST multipart form: %s", err)
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "%s\n", unknownError)
			return
		}
		var content string
		if vs, found := r.Form["paste"]; found {
			content = vs[0]
		} else {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "%s\n", missingForm)
			return
		}
		dir, _ := path.Split(pastePath)
		if err = os.MkdirAll(dir, 0700); err != nil {
			log.Printf("Could not create directories leading to %s: %s", pastePath, err)
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "%s\n", unknownError)
			return
		}
		timer := time.NewTimer(lifeTime)
		go func() {
			<-timer.C
			endLife(id)
		}()
		pasteFile, err := os.OpenFile(pastePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			log.Printf("Could not create new paste pasteFile %s: %s", pastePath, err)
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "%s\n", unknownError)
			return
		}
		compWriter := zlib.NewWriter(pasteFile)
		_, err = io.WriteString(compWriter, content)
		compWriter.Close()
		pasteFile.Close()
		if err != nil {
			log.Printf("Could not write compressed data into %s: %s", pastePath, err)
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "%s\n", unknownError)
			return
		}
		log.Printf("Created a new paste: %s", id)
		fmt.Fprintf(w, "%s/%s\n", siteUrl, id)
	}
}

func main() {
	var err error
	if indexTemplate, err = template.ParseFiles(indexTmpl); err != nil {
		log.Printf("Could not load template %s: %s", indexTmpl, err)
		return
	}
	if err = os.RemoveAll(dataDir); err != nil {
		log.Printf("Could not clean data directory %s: %s", dataDir, err)
		return
	}
	if err = os.Mkdir(dataDir, 0700); err != nil {
		log.Printf("Could not create data directory %s: %s", dataDir, err)
		return
	}
	if err = os.Chdir(dataDir); err != nil {
		log.Printf("Could not enter data directory %s: %s", dataDir, err)
		return
	}
	http.HandleFunc("/", handler)
	http.ListenAndServe(listen, nil)
}
