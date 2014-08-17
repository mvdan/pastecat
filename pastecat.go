/* Copyright (c) 2014, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
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
	"sync"
	"time"
)

const (
	indexTmpl = "index.html"
	formTmpl  = "form.html"
	chars     = "abcdefghijklmnopqrstuvwxyz0123456789"
	randTries = 10

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
	data          = struct {
		sync.RWMutex
		m map[Id]PasteInfo
	}{m: make(map[Id]PasteInfo)}
	customRand *rand.Rand
)

func init() {
	flag.StringVar(&siteUrl, "u", "http://localhost:8080", "URL of the site")
	flag.StringVar(&listen, "l", "localhost:8080", "Host and port to listen to")
	flag.StringVar(&dataDir, "d", "data", "Directory to store all the pastes in")
	flag.DurationVar(&lifeTime, "t", 12*time.Hour, "Lifetime of the pastes (units: s,m,h)")
	flag.StringVar(&maxSizeStr, "s", "1M", "Maximum size of POSTs in bytes (units: B,K,M)")
	flag.IntVar(&idSize, "i", 8, "Size of the paste ids (between 6 and 256)")
	validId = regexp.MustCompile("^[a-zA-Z0-9]{" + strconv.Itoa(idSize) + "}$")
	customRand = rand.New(rand.NewSource(time.Now().UnixNano()))
}

type PasteInfo struct {
	ModTime     time.Time
	DeathTime   time.Time
	Size        ByteSize
	Etag        string
	ContentType string
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

func RandomId() (Id, error) {
	s := make([]byte, idSize)
	var id Id
	data.RLock()
	for try := 0; try < randTries; try++ {
		var offset int = 0
	RandLoop:
		for {
			r := customRand.Int63()
			for i := 0; i < 8; i++ {
				randbyte := int(r&0xff) % len(chars)
				s[offset] = chars[randbyte]
				offset++
				if offset == idSize {
					break RandLoop
				}
				r >>= 8
			}
		}
		id = Id(s)
		if _, e := data.m[id]; !e {
			data.RUnlock()
			return id, nil
		}
	}
	return id, fmt.Errorf("Gave up trying to find an unused random id after %d tries", randTries)
}

func (id Id) String() string {
	return string(id)
}

func (id Id) Path() string {
	return path.Join(string(id[0:2]), string(id[2:4]), string(id[4:]))
}

func (id Id) EndLife() {
	data.Lock()
	err := os.Remove(id.Path())
	if err == nil {
		delete(data.m, id)
		data.Unlock()
		log.Printf("Removed paste: %s", id)
	} else {
		data.Unlock()
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
			indexTemplate.Execute(w, struct {
				SiteUrl, LifeTime string
			}{
				siteUrl,
				fmt.Sprintf("%g hours and %g minutes", lifeTime.Hours(),
					lifeTime.Minutes()-lifeTime.Hours()*60),
			})
			return
		case "/form":
			formTemplate.Execute(w, struct{ SiteUrl string }{siteUrl})
			return
		}
		rawId := r.URL.Path[1:]
		if !validId.MatchString(rawId) {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "%s\n", invalidId)
			return
		}
		id := Id(strings.ToLower(rawId))
		data.RLock()
		pasteInfo, e := data.m[id]
		if !e {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, "%s\n", pasteNotFound)
			return
		}
		if inm := r.Header.Get("If-None-Match"); inm != "" {
			if pasteInfo.Etag == inm || inm == "*" {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		pastePath := id.Path()
		pasteFile, err := os.Open(pastePath)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "%s\n", unknownError)
			return
		}
		defer pasteFile.Close()
		w.Header().Set("Etag", pasteInfo.Etag)
		w.Header().Set("Content-Type", pasteInfo.ContentType)
		http.ServeContent(w, r, "", pasteInfo.ModTime, pasteFile)
		data.RUnlock()

	case "POST":
		r.Body = http.MaxBytesReader(w, r.Body, int64(maxSize))
		var id Id
		var content string
		id, err = RandomId()
		if err != nil {
			log.Println(err)
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
		if vs, found := r.Form["paste"]; found && len(vs[0]) > 0 {
			content = vs[0]
		} else {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "%s\n", missingForm)
			return
		}
		pastePath := id.Path()
		dir, _ := path.Split(pastePath)
		if err = os.MkdirAll(dir, 0700); err != nil {
			log.Printf("Could not create directories leading to %s: %s", pastePath, err)
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "%s\n", unknownError)
			return
		}
		deathTime := time.Now().Add(lifeTime)
		id.EndLifeAfter(lifeTime)
		pasteFile, err := os.OpenFile(pastePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			log.Printf("Could not create new paste pasteFile %s: %s", pastePath, err)
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "%s\n", unknownError)
			return
		}
		defer pasteFile.Close()
		b, err := io.WriteString(pasteFile, content)
		if err != nil {
			log.Printf("Could not write data into %s: %s", pastePath, err)
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "%s\n", unknownError)
			return
		}
		writtenSize := ByteSize(b)
		pasteInfo := PasteInfo{
			ModTime:   time.Now(),
			DeathTime: deathTime,
			Size:      writtenSize,
		}
		pasteInfo.Etag = fmt.Sprintf("%d-%s", pasteInfo.ModTime.Unix(), id)
		pasteInfo.ContentType = http.DetectContentType([]byte(content))
		if pasteInfo.ContentType == "application/octet-stream" {
			pasteInfo.ContentType = "text-plain; charset=utf-8"
		}
		data.Lock()
		data.m[id] = pasteInfo
		data.Unlock()
		log.Printf("Created new paste %s (%s %s) to die at %s",
			id, pasteInfo.ContentType, pasteInfo.Size, pasteInfo.DeathTime)
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
	size := ByteSize(fileInfo.Size())
	pasteFile, err := os.Open(filePath)
	if err != nil {
		return err
	}
	read := make([]byte, 512)
	_, err = pasteFile.Read(read)
	if err != nil {
		return err
	}
	pasteInfo := PasteInfo{
		ModTime:   modTime,
		DeathTime: deathTime,
		Size:      size,
		Etag:      fmt.Sprintf("%d-%s", modTime.Unix(), id),
	}
	pasteInfo.ContentType = http.DetectContentType(read)
	if pasteInfo.ContentType == "application/octet-stream" {
		pasteInfo.ContentType = "text-plain; charset=utf-8"
	}
	data.m[id] = pasteInfo
	log.Printf("Recovered paste %s (%s %s) from %s has %s left",
		id, pasteInfo.ContentType, pasteInfo.Size, pasteInfo.ModTime, lifeLeft)
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
	log.Printf("idSize   = %d", idSize)
	log.Printf("maxSize  = %s", maxSize)
	log.Printf("siteUrl  = %s", siteUrl)
	log.Printf("listen   = %s", listen)
	log.Printf("dataDir  = %s", dataDir)
	log.Printf("lifeTime = %s", lifeTime)
	if err = filepath.Walk(".", walkFunc); err != nil {
		log.Fatalf("Could not recover data directory %s: %s", dataDir, err)
	}
	http.HandleFunc("/", handler)
	log.Fatal(http.ListenAndServe(listen, nil))
}
