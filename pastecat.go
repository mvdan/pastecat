/* Copyright (c) 2014, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"compress/zlib"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	indexTmpl = "index.html"
	formTmpl  = "form.html"
	chars     = "abcdefghijklmnopqrstuvwxyz0123456789"

	// GET error messages
	invalidId     = "Invalid paste id."
	pasteNotFound = "Paste doesn't exist."
	unknownError  = "Something went terribly wrong."
	// POST error messages
	missingForm = "Paste could not be found inside the posted form."
)

var (
	siteUrl, listen, dataDir string
	lifeTime                 time.Duration
	maxSizeStr               string
	idSize                   int
	maxSize                  ByteSize

	validId       *regexp.Regexp
	regexByteSize = regexp.MustCompile(`^([\d\.]+)\s*([KM]?B|[BKM])$`)
	indexTemplate *template.Template
	formTemplate  *template.Template
	pasteInfos    = make(map[Id]PasteInfo)
)

func init() {
	flag.StringVar(&siteUrl, "u", "http://localhost:8080", "URL of the site")
	flag.StringVar(&listen, "l", "localhost:8080", "Host and port to listen to")
	flag.StringVar(&dataDir, "d", "data", "Directory to store all the pastes in")
	flag.DurationVar(&lifeTime, "t", 12*time.Hour, "Lifetime of the pastes (units: s,m,h)")
	flag.StringVar(&maxSizeStr, "s", "1M", "Maximum size of POSTs in bytes (units: B,K,M)")
	flag.IntVar(&idSize, "i", 8, "Size of the paste ids (between 6 and 256)")
	validId = regexp.MustCompile("^[a-zA-Z0-9]{" + strconv.Itoa(idSize) + "}$")
}

type PasteInfo struct {
	ModTime time.Time
}

type Id string

func IdFromPath(idPath string) (Id, error) {
	parts := strings.Split(idPath, string(filepath.Separator))
	if len(parts) != 3 {
		return "", errors.New("Found invalid number of directories at " + idPath)
	}
	rawId := parts[0] + parts[1] + parts[2]
	if !validId.MatchString(rawId) {
		return "", errors.New("Found invalid id " + rawId)
	}
	return Id(rawId), nil
}

func RandomId() Id {
	s := make([]byte, idSize)
	var offset int = 0
MainLoop:
	for {
		r := rand.Int63()
		for i := 0; i < 8; i++ {
			randbyte := int(r&0xff) % len(chars)
			s[offset] = chars[randbyte]
			offset++
			if offset == idSize {
				break MainLoop
			}
			r >>= 8
		}
	}
	return Id(s)
}

func (id Id) String() string {
	return string(id)
}

func (id Id) Path() string {
	return path.Join(string(id[0:2]), string(id[2:4]), string(id[4:]))
}

func (id Id) EndLife() {
	err := os.Remove(id.Path())
	if err == nil {
		delete(pasteInfos, id)
		log.Printf("Removed paste: %s", id)
	} else {
		log.Printf("Could not end the life of %s: %s", id, err)
		id.EndLifeAfter(2 * time.Minute)
	}
}

func (id Id) EndLifeAfter(duration time.Duration) {
	timer := time.NewTimer(duration)
	go func() {
		<-timer.C
		id.EndLife()
	}()
}

type ByteSize int64

const (
	B ByteSize = 1 << (10 * iota)
	KB
	MB
)

func parseByteSize(str string) (ByteSize, error) {
	if !regexByteSize.MatchString(str) {
		return 0, errors.New("Could not parse size in bytes")
	}
	parts := regexByteSize.FindStringSubmatch(str)
	size, _ := strconv.ParseFloat(string(parts[1]), 64)

	switch string(parts[2]) {
	case "KB", "K":
		size *= float64(KB)
	case "MB", "M":
		size *= float64(MB)
	}
	return ByteSize(size), nil
}

func (b ByteSize) String() string {
	switch {
	case b >= MB:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(KB))
	}
	return fmt.Sprintf("%d B", b)
}

func handler(w http.ResponseWriter, r *http.Request) {
	var err error
	switch r.Method {
	case "GET":
		switch r.URL.Path {
		case "/":
			indexTemplate.Execute(w, siteUrl)
			return
		case "/form":
			formTemplate.Execute(w, siteUrl)
			return
		}
		rawId := r.URL.Path[1:]
		if !validId.MatchString(rawId) {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "%s\n", invalidId)
			return
		}
		id := Id(strings.ToLower(rawId))
		_, e := pasteInfos[id]
		if !e {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, "%s\n", pasteNotFound)
			return
		}
		pastePath := id.Path()
		pasteFile, err := os.Open(pastePath)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "%s\n", unknownError)
			return
		}
		defer pasteFile.Close()
		compReader, err := zlib.NewReader(pasteFile)
		if err != nil {
			log.Printf("Could not open a compression reader for %s: %s", pastePath, err)
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "%s\n", unknownError)
			return
		}
		defer compReader.Close()
		io.Copy(w, compReader)

	case "POST":
		r.Body = http.MaxBytesReader(w, r.Body, int64(maxSize))
		var id Id
		var pastePath, content string
		found := false
		for i := 0; i < 10; i++ {
			id = RandomId()
			if _, e := pasteInfos[id]; !e {
				found = true
				break
			}
		}
		if !found {
			log.Printf("Gave up trying to find an unused random id")
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "%s\n", unknownError)
			return
		}
		if err = r.ParseMultipartForm(int64(maxSize)); err != nil {
			log.Printf("Could not parse POST multipart form: %s", err)
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "%s\n", err)
			return
		}
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
		id.EndLifeAfter(lifeTime)
		pasteFile, err := os.OpenFile(pastePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			log.Printf("Could not create new paste pasteFile %s: %s", pastePath, err)
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "%s\n", unknownError)
			return
		}
		defer pasteFile.Close()
		compWriter := zlib.NewWriter(pasteFile)
		defer compWriter.Close()
		b, err := io.WriteString(compWriter, content)
		if err != nil {
			log.Printf("Could not write compressed data into %s: %s", pastePath, err)
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "%s\n", unknownError)
			return
		}
		writtenSize := ByteSize(b)
		pasteInfos[id] = PasteInfo{ModTime: time.Now()}
		log.Printf("Created a new paste: %s (%s)", id, writtenSize)
		fmt.Fprintf(w, "%s/%s\n", siteUrl, id)
	}
}

func walkFunc(filePath string, fileInfo os.FileInfo, err error) error {
	if err != nil {
		return err
	}
	if fileInfo.IsDir() {
		return nil
	}
	id, err := IdFromPath(filePath)
	if err != nil {
		return err
	}
	modTime := fileInfo.ModTime()
	deathTime := modTime.Add(lifeTime)
	now := time.Now()
	if deathTime.Before(now) {
		go id.EndLife()
		return nil
	}
	var lifeLeft time.Duration
	if deathTime.After(now.Add(lifeTime)) {
		lifeLeft = lifeTime
	} else {
		lifeLeft = deathTime.Sub(now)
	}
	log.Printf("Recovered paste %s has %s left", id, lifeLeft)
	pasteInfos[id] = PasteInfo{ModTime: modTime}
	id.EndLifeAfter(lifeLeft)
	return nil
}

func main() {
	var err error
	flag.Parse()
	if idSize < 6 || idSize > 256 {
		log.Fatalf("Provided id size %d is not between 6 and 256", idSize)
	}
	if maxSize, err = parseByteSize(maxSizeStr); err != nil {
		log.Fatalf("Invalid max size '%s': %s", maxSizeStr, err)
	}
	if indexTemplate, err = template.ParseFiles(indexTmpl); err != nil {
		log.Fatalf("Could not load template %s: %s", indexTmpl, err)
	}
	if formTemplate, err = template.ParseFiles(formTmpl); err != nil {
		log.Fatalf("Could not load template %s: %s", formTmpl, err)
	}
	if err = os.MkdirAll(dataDir, 0700); err != nil {
		log.Fatalf("Could not create data directory %s: %s", dataDir, err)
	}
	if err = os.Chdir(dataDir); err != nil {
		log.Fatalf("Could not enter data directory %s: %s", dataDir, err)
	}
	if err = filepath.Walk(".", walkFunc); err != nil {
		log.Fatalf("Could not recover data directory %s: %s", dataDir, err)
	}
	log.Printf("idSize   = %d", idSize)
	log.Printf("maxSize  = %s", maxSize)
	log.Printf("siteUrl  = %s", siteUrl)
	log.Printf("listen   = %s", listen)
	log.Printf("dataDir  = %s", dataDir)
	log.Printf("lifeTime = %s", lifeTime)
	http.HandleFunc("/", handler)
	log.Fatal(http.ListenAndServe(listen, nil))
}
