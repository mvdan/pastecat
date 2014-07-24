/* Copyright (c) 2014, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"compress/gzip"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
)

const (
	// Main config options
	chars   = "abcdefghijklmnopqrstuvwxyz0123456789"
	idSize  = 8
	siteUrl = "http://localhost:9090"
	listen  = "localhost:9090"
	dataDir = "data"
	maxSize = 1 << 20

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

func handler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		id := r.URL.Path[1:]
		if len(id) == 0 {
			fmt.Fprintf(w, "<html><body><form action=\"%s\" method=\"POST\"><textarea cols=80 rows=48 name=\"paste\"></textarea><br><button type=\"submit\">paste</button></form></body></html>", siteUrl)
			return
		}
		if !validId.MatchString(id) {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "%s\n", invalidId)
			return
		}
		filePath := pathId(id)
		file, err := os.Open(filePath)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, "%s\n", pasteNotFound)
			return
		}
		compReader, err := gzip.NewReader(file)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "%s\n", unknownError)
			return
		}
		io.Copy(w, compReader)
		compReader.Close()
		file.Close()

	case "POST":
		r.Body = http.MaxBytesReader(w, r.Body, maxSize)
		var id, filePath string
		for {
			id = randomId()
			filePath = pathId(id)
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				break
			}
		}
		dir, _ := path.Split(filePath)
		err := os.MkdirAll(dir, 0700)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "%s\n", unknownError)
			return
		}
		file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "%s\n", unknownError)
			return
		}
		err = r.ParseMultipartForm(maxSize << 1)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "%s\n", unknownError)
			return
		}
		vs, found := r.Form["paste"]
		if !found {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "%s\n", missingForm)
			return
		}
		data := vs[0]
		compWriter := gzip.NewWriter(file)
		_, err = io.WriteString(compWriter, data)
		compWriter.Close()
		file.Close()
		if err != nil {
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
		return
	}
	http.HandleFunc("/", handler)
	http.ListenAndServe(listen, nil)
}
