/* Copyright (c) 2014, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
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
	idSize    = 8
	rawIdSize = idSize / 2
	randTries = 10

	// GET error messages
	invalidId     = "Invalid paste id."
	pasteNotFound = "Paste doesn't exist."
	unknownError  = "Something went terribly wrong."
	// POST error messages
	missingForm = "Paste could not be found inside the posted form."

	// Common error messages
	timedOut = "Request timed out."
)

var (
	siteUrl, listen, dataDir, maxSizeStr string
	lifeTime, timeout                    time.Duration
	maxSize                              ByteSize
	indexTemplate, formTemplate          *template.Template

	regexByteSize = regexp.MustCompile(`^([\d\.]+)\s*([KM]?B|[BKM])$`)
	startTime     = time.Now()
)

// Default is "text-plain; charset=utf-8"
var mimeTypes = map[string]string{
	".gif":  "image/gif",
	".jpg":  "image/jpeg",
	".png":  "image/png",
	".pdf":  "application/pdf",
}

var workers [256]Worker
var post = make(chan PostRequest) // Posting is shared to balance load

type Id [rawIdSize]byte

type PasteInfo struct {
	Etag, ContentType, Path string
	ModTime                 time.Time
}

type GetRequest struct {
	w    http.ResponseWriter
	r    *http.Request
	done chan struct{}
	id   Id
}

type PostRequest struct {
	w       http.ResponseWriter
	r       *http.Request
	done    chan struct{}
	content []byte
	ext     string
	modTime time.Time
}

type Worker struct {
	n    byte // Its number, aka the first two hex chars
	get  chan GetRequest
	del  chan Id
	m    map[Id]PasteInfo
}

func (w Worker) recoverPaste(filePath string, fileInfo os.FileInfo, err error) error {
	if err != nil {
		return err
	}
	if fileInfo.IsDir() {
		return nil
	}
	dirParts := strings.Split(filePath, string(filepath.Separator))
	if len(dirParts) != 2 {
		return errors.New("Found invalid number of directories at " + filePath)
	}
	hexId := dirParts[0] + dirParts[1]
	ext := filepath.Ext(hexId)
	id, err := IdFromString(hexId[:len(hexId)-len(ext)])
	if err != nil {
		return err
	}
	modTime := fileInfo.ModTime()
	deathTime := modTime.Add(lifeTime)
	if deathTime.Before(startTime) {
		err := os.Remove(filePath)
		if err != nil {
			return err
		}
	}
	if modTime.After(startTime) {
		modTime = startTime
	}
	w.m[id] = id.GenPasteInfo(modTime, ext)
	w.DeletePasteAfter(id, deathTime.Sub(startTime))
	return nil
}

func (w Worker) RandomId() (Id, error) {
	var id Id
	id[0] = w.n
	for try := 0; try < randTries; try++ {
		if _, err := rand.Read(id[1:]); err != nil {
			return id, err
		}
		if _, e := w.m[id]; !e {
			return id, nil
		}
	}
	return id, fmt.Errorf("Gave up trying to find an unused random id after %d tries", randTries)
}

func (w Worker) Work() {
	dir := hex.EncodeToString([]byte{w.n})
	if stat, err := os.Stat(dir); err == nil {
		if !stat.IsDir() {
			log.Fatalf("%s/%s exists but is not a directory!", dataDir, dir)
		}
	} else {
		if err := os.Mkdir(dir, 0700); err != nil {
			log.Fatalf("Could not create data directory %s/%s: %s", dataDir, dir, err)
		}
	}
	w.m = make(map[Id]PasteInfo)
	if err := filepath.Walk(dir, w.recoverPaste); err != nil {
		log.Fatalf("Could not recover data directory %s/%s: %s", dataDir, dir, err)
	}
	for {
		var done chan struct{}
		select {
		case request := <-w.get:
			done = request.done
			pasteInfo, e := w.m[request.id]
			if !e {
				http.Error(request.w, pasteNotFound, http.StatusNotFound)
				break
			}
			if inm := request.r.Header.Get("If-None-Match"); inm != "" {
				if pasteInfo.Etag == inm || inm == "*" {
					request.w.WriteHeader(http.StatusNotModified)
					break
				}
			}
			pasteFile, err := os.Open(pasteInfo.Path)
			if err != nil {
				http.Error(request.w, unknownError, http.StatusInternalServerError)
				break
			}
			request.w.Header().Set("Etag", pasteInfo.Etag)
			request.w.Header().Set("Content-Type", pasteInfo.ContentType)
			http.ServeContent(request.w, request.r, "", pasteInfo.ModTime, pasteFile)
			pasteFile.Close()

		case request := <-post:
			done = request.done
			id, err := w.RandomId()
			if err != nil {
				log.Println(err)
				http.Error(request.w, unknownError, http.StatusInternalServerError)
				break
			}
			pasteInfo := id.GenPasteInfo(request.modTime, request.ext)
			pasteFile, err := os.OpenFile(pasteInfo.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				log.Printf("Could not create new paste file %s: %s", pasteInfo.Path, err)
				http.Error(request.w, unknownError, http.StatusInternalServerError)
				break
			}
			_, err = pasteFile.Write(request.content)
			pasteFile.Close()
			if err != nil {
				log.Printf("Could not write data into %s: %s", pasteInfo.Path, err)
				http.Error(request.w, unknownError, http.StatusInternalServerError)
				break
			}
			w.m[id] = pasteInfo
			w.DeletePasteAfter(id, lifeTime)
			fmt.Fprintf(request.w, "%s/%s\n", siteUrl, id)

		case id := <-w.del:
			pasteInfo, _ := w.m[id]
			if err := os.Remove(pasteInfo.Path); err == nil {
				delete(w.m, id)
			} else {
				log.Printf("Could not remove %s: %s", id, err)
				w.DeletePasteAfter(id, 2*time.Minute)
			}
		}
		if done != nil {
			done <- struct{}{}
		}
	}
}

func init() {
	flag.StringVar(&siteUrl, "u", "http://localhost:8080", "URL of the site")
	flag.StringVar(&listen, "l", "localhost:8080", "Host and port to listen to")
	flag.StringVar(&dataDir, "d", "data", "Directory to store all the pastes in")
	flag.DurationVar(&lifeTime, "t", 12*time.Hour, "Lifetime of the pastes")
	flag.DurationVar(&timeout, "T", 200*time.Millisecond, "Timeout of requests")
	flag.StringVar(&maxSizeStr, "s", "1M", "Maximum size of POSTs in bytes")
}

func IdFromString(hexId string) (Id, error) {
	var id Id
	if len(hexId) != idSize {
		return id, errors.New("Invalid id at " + hexId)
	}
	b, err := hex.DecodeString(hexId)
	if err != nil || len(b) != rawIdSize {
		return id, errors.New("Invalid id at " + hexId)
	}
	copy(id[:], b)
	return id, nil
}

func (id Id) String() string {
	return hex.EncodeToString(id[:])
}

func (id Id) GenPasteInfo(modTime time.Time, ext string) (pasteInfo PasteInfo) {
	pasteInfo.ModTime = modTime
	pasteInfo.Etag = fmt.Sprintf("%d-%s", pasteInfo.ModTime.Unix(), id)
	var e bool
	if pasteInfo.ContentType, e = mimeTypes[ext]; !e {
		pasteInfo.ContentType = "text-plain; charset=utf-8"
	}
	hexId := id.String()
	pasteInfo.Path = path.Join(hexId[0:2], hexId[2:] + ext)
	return
}

func (w Worker) DeletePasteAfter(id Id, duration time.Duration) {
	timer := time.NewTimer(duration)
	go func() {
		<-timer.C
		w.del <- id
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
		return fmt.Sprintf("%.2fMB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.2fKB", float64(b)/float64(KB))
	}
	return fmt.Sprintf("%dB", b)
}

func handler(w http.ResponseWriter, r *http.Request) {
	done := make(chan struct{})
	timer := time.NewTimer(timeout)
	switch r.Method {
	case "GET":
		switch r.URL.Path {
		case "/":
			indexTemplate.Execute(w, struct{ SiteUrl, LifeTime string }{
				siteUrl, lifeTime.String()})
			return
		case "/form":
			formTemplate.Execute(w, struct{ SiteUrl string }{siteUrl})
			return
		}
		id, err := IdFromString(r.URL.Path[1:])
		if err != nil {
			http.Error(w, invalidId, http.StatusBadRequest)
			return
		}
		select {
		case <-timer.C:
			http.Error(w, timedOut, http.StatusRequestTimeout)
		case workers[id[0]].get <- GetRequest{id: id, w: w, r: r, done: done}:
			// request is sent
			timer.Stop()
		}

	case "POST":
		r.Body = http.MaxBytesReader(w, r.Body, int64(maxSize))
		var content []byte
		ext := ""
		if value := r.FormValue("paste"); value != "" {
			content = []byte(value)
		} else if f, h, err := r.FormFile("paste"); err == nil {
			content, err = ioutil.ReadAll(f)
			ext = strings.ToLower(filepath.Ext(h.Filename))
			f.Close()
			if err != nil {
				http.Error(w, missingForm, http.StatusBadRequest)
				return
			}
		} else {
			http.Error(w, missingForm, http.StatusBadRequest)
			return
		}
		select {
		case <-timer.C:
			http.Error(w, timedOut, http.StatusRequestTimeout)
		case post <- PostRequest{content: content, ext: ext, modTime: time.Now(), w: w, r: r, done: done}:
			// request is sent
			timer.Stop()
		}

	default:
		http.Error(w, "Unsupported action.", http.StatusBadRequest)
		return
	}
	<-done
}

func main() {
	var err error
	flag.Parse()
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
	log.Printf("maxSize  = %s", maxSize)
	log.Printf("siteUrl  = %s", siteUrl)
	log.Printf("listen   = %s", listen)
	log.Printf("dataDir  = %s", dataDir)
	log.Printf("lifeTime = %s", lifeTime)
	log.Printf("timeout  = %s", timeout)
	for n := range workers {
		w := &workers[n]
		w.n = byte(n)
		w.get = make(chan GetRequest)
		w.del = make(chan Id)
		go w.Work()
	}
	http.HandleFunc("/", handler)
	log.Printf("Up and running!")
	log.Fatal(http.ListenAndServe(listen, nil))
}
