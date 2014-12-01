/* Copyright (c) 2014, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// Length of the random hexadecimal ids assigned to pastes. At least 4.
	idSize = 8
	// Number of times to try getting a random paste id
	randTries = 10
	// Name of the HTTP form field when uploading a paste
	fieldName = "paste"
	// Content-Type when serving pastes
	contentType = "text/plain; charset=utf-8"
	// Report usage stats how often
	statsReport = 1 * time.Minute
	// How long to wait before retrying to delete a file
	deleteRetry = 2 * time.Minute

	// GET error messages
	invalidID = "invalid paste id"

	// Common error messages
	unknownAction = "unsupported action"
)

var (
	siteURL, listen           string
	lifeTime                  time.Duration
	maxNumber                 int
	maxSizeStr, maxStorageStr string
	maxSize, maxStorage       ByteSize
	templates                 *template.Template

	regexByteSize = regexp.MustCompile(`^([\d\.]+)\s*([KMGT]?B?)$`)
	startTime     = time.Now()

	store Store
)

func init() {
	flag.StringVar(&siteURL, "u", "http://localhost:8080", "URL of the site")
	flag.StringVar(&listen, "l", ":8080", "Host and port to listen to")
	flag.DurationVar(&lifeTime, "t", 24*time.Hour, "Lifetime of the pastes")
	flag.StringVar(&maxSizeStr, "s", "1M", "Maximum size of pastes")
	flag.IntVar(&maxNumber, "m", 0, "Maximum number of pastes to store at once")
	flag.StringVar(&maxStorageStr, "M", "1G", "Maximum storage size to use at once")
}

type ByteSize int64

const (
	_ ByteSize = 1 << (10 * iota)
	kbyte
	mbyte
	gbyte
	tbyte
)

func parseByteSize(str string) (ByteSize, error) {
	if !regexByteSize.MatchString(str) {
		return 0, errors.New("Could not parse size in bytes")
	}
	parts := regexByteSize.FindStringSubmatch(str)
	size, _ := strconv.ParseFloat(string(parts[1]), 64)
	switch string(parts[2]) {
	case "KB", "K":
		size *= float64(kbyte)
	case "MB", "M":
		size *= float64(mbyte)
	case "GB", "G":
		size *= float64(gbyte)
	case "TB", "T":
		size *= float64(tbyte)
	}
	return ByteSize(size), nil
}

func (b ByteSize) String() string {
	switch {
	case b >= tbyte:
		return fmt.Sprintf("%.2fGB", float64(b)/float64(tbyte))
	case b >= gbyte:
		return fmt.Sprintf("%.2fGB", float64(b)/float64(gbyte))
	case b >= mbyte:
		return fmt.Sprintf("%.2fMB", float64(b)/float64(mbyte))
	case b >= kbyte:
		return fmt.Sprintf("%.2fKB", float64(b)/float64(kbyte))
	}
	return fmt.Sprintf("%dB", b)
}

type ID [idSize / 2]byte

func IDFromString(hexID string) (id ID, err error) {
	if len(hexID) != idSize {
		return id, errors.New("Invalid id at " + hexID)
	}
	b, err := hex.DecodeString(hexID)
	if err != nil || len(b) != idSize/2 {
		return id, errors.New("Invalid id at " + hexID)
	}
	copy(id[:], b)
	return id, nil
}

func (id ID) String() string {
	return hex.EncodeToString(id[:])
}

type Header struct {
	Etag, Expires string
	ModTime       time.Time
	Size          ByteSize
}

type Content interface {
	io.Reader
	io.ReaderAt
	io.Seeker
	io.Closer
}

func describeLimits() string {
	var limits []string
	if maxSize > 0 {
		limits = append(limits, fmt.Sprintf("Maximum size per paste is %s.", maxSize))
	}
	if lifeTime > 0 {
		limits = append(limits, fmt.Sprintf("Pastes will be deleted after %s.", lifeTime))
	}
	if len(limits) > 0 {
		return strings.Join(limits, " ") + "\n\n"
	}
	return ""
}

func getContentFromForm(r *http.Request) (content []byte, err error) {
	if value := r.FormValue(fieldName); value != "" {
		return []byte(value), nil
	}
	if f, _, err := r.FormFile(fieldName); err == nil {
		defer f.Close()
		content, err = ioutil.ReadAll(f)
		if err == nil {
			return content, nil
		}
	}
	return content, err
}

func setupPasteDeletion(id ID) {
	if lifeTime == 0 {
		return
	}
	timer := time.NewTimer(lifeTime)
	go func() {
		for {
			<-timer.C
			err := store.Delete(id)
			if err == nil {
				break
			}
			timer.Reset(deleteRetry)
		}
	}()
}

func handler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		switch r.URL.Path {
		case "/":
			templates.ExecuteTemplate(w, "index.html",
				struct{ SiteURL, LimitDesc, FieldName string }{
					siteURL, describeLimits(), fieldName})
			return
		case "/form":
			templates.ExecuteTemplate(w, "form.html",
				struct{ SiteURL, LimitDesc, FieldName string }{
					siteURL, describeLimits(), fieldName})
			return
		}
		id, err := IDFromString(r.URL.Path[1:])
		if err != nil {
			http.Error(w, invalidID, http.StatusBadRequest)
			return
		}
		content, header, err := store.Get(id)
		if err == ErrPasteNotFound {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		} else if err != nil {
			log.Printf("Unknown store.Get() error: %s", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer content.Close()
		w.Header().Set("Etag", header.Etag)
		if lifeTime > 0 {
			w.Header().Set("Expires", header.Expires)
		}
		w.Header().Set("Content-Type", contentType)
		http.ServeContent(w, r, "", header.ModTime, content)

	case "POST":
		r.Body = http.MaxBytesReader(w, r.Body, int64(maxSize))
		content, err := getContentFromForm(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		id, err := store.Put(content)
		if err == ErrReachedMax {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		} else if err != nil {
			log.Printf("Unknown store.Put() error: %s", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		setupPasteDeletion(id)
		fmt.Fprintf(w, "%s/%s\n", siteURL, id)

	default:
		http.Error(w, unknownAction, http.StatusBadRequest)
		return
	}
}

func main() {
	var err error
	flag.Parse()
	if maxSize, err = parseByteSize(maxSizeStr); err != nil {
		log.Fatalf("Invalid max size '%s': %s", maxSizeStr, err)
	}
	if maxStorage, err = parseByteSize(maxStorageStr); err != nil {
		log.Fatalf("Invalid max storage '%s': %s", maxStorageStr, err)
	}
	templates = template.Must(template.ParseFiles("index.html", "form.html"))

	log.Printf("siteURL    = %s", siteURL)
	log.Printf("listen     = %s", listen)
	log.Printf("lifeTime   = %s", lifeTime)
	log.Printf("maxSize    = %s", maxSize)
	log.Printf("maxNumber  = %d", maxNumber)
	log.Printf("maxStorage = %s", maxStorage)

	args := flag.Args()
	storageType := "fs"
	if len(args) > 0 {
		storageType = args[0]
		args = args[1:]
	}
	switch storageType {
	case "fs":
		if len(args) > 1 {
			log.Fatalf("Too many arguments given for %s", storageType)
		}
		pasteDir := "pastes"
		if len(args) > 0 {
			pasteDir = args[0]
		}
		log.Printf("Starting up file store in the directory '%s'", pasteDir)
		store, err = newFileStore(pasteDir, maxNumber, maxStorage, lifeTime)
	case "mem":
		if len(args) > 0 {
			log.Fatalf("Too many arguments given for %s", storageType)
		}
		log.Printf("Starting up in-memory store")
		store, err = newMemStore(maxNumber, maxStorage, lifeTime)
	default:
		log.Fatalf("Unknown paste store type '%s'", storageType)
	}
	if err != nil {
		log.Fatalf("Could not start %s paste store: %s", storageType, err)
	}

	log.Println(store.Report())
	ticker := time.NewTicker(statsReport)
	go func() {
		for _ = range ticker.C {
			log.Println(store.Report())
		}
	}()
	http.HandleFunc("/", handler)
	log.Println("Up and running!")
	log.Fatal(http.ListenAndServe(listen, nil))
}
