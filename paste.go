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
	chars   = "abcdefghijklmnopqrstuvwxyz0123456789"
	idSize  = 8
	siteUrl = "http://paste.cat"
	listen  = "localhost:9090"
	dataDir = "data"

	invalidId     = "Invalid paste id."
	pasteNotFound = "Paste doesn't exist."
	unknownError  = "Something went wrong. Woop woop woop woop!"
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
			s[offset] = chars[(r&0xff)%36]
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
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
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
		data := r.FormValue("paste")
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
