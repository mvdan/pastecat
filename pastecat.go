/* Copyright (c) 2014-2015, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/mvdan/bytesize"
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

	invalidID     = "invalid paste id"
	unknownAction = "unsupported action"
)

var (
	siteURL, listen string
	lifeTime        time.Duration
	maxNumber       int

	maxSize    = 1 * bytesize.MB
	maxStorage = 1 * bytesize.GB

	templates *template.Template
	startTime = time.Now()

	store Store
)

func init() {
	flag.StringVar(&siteURL, "u", "http://localhost:8080", "URL of the site")
	flag.StringVar(&listen, "l", ":8080", "Host and port to listen to")
	flag.DurationVar(&lifeTime, "t", 24*time.Hour, "Lifetime of the pastes")
	flag.IntVar(&maxNumber, "m", 0, "Maximum number of pastes to store at once")
	flag.Var(&maxSize, "s", "Maximum size of pastes")
	flag.Var(&maxStorage, "M", "Maximum storage size to use at once")
}

// Binary representation of an identifier for a paste
type ID [idSize / 2]byte

// Parse a hexadecimal string into an ID. Return the resulting ID and an
// error, if any.
func IDFromString(hexID string) (id ID, err error) {
	if len(hexID) != idSize {
		return id, fmt.Errorf("invalid id at %s", hexID)
	}
	b, err := hex.DecodeString(hexID)
	if err != nil || len(b) != idSize/2 {
		return id, fmt.Errorf("invalid id at %s", hexID)
	}
	copy(id[:], b)
	return id, nil
}

func (id ID) String() string {
	return hex.EncodeToString(id[:])
}

// Static paste attributes
type Header struct {
	Etag, Expires string
	ModTime       time.Time
	Size          int64
}

// Interface for accessing the paste's content
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
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxSize))
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
	return nil, err
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
			log.Printf("Unknown error on GET: %s", err)
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
		content, err := getContentFromForm(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		id, err := store.Put(content)
		if err == ErrReachedMaxNumber || err == ErrReachedMaxStorage {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		} else if err != nil {
			log.Printf("Unknown error on POST: %s", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		setupPasteDeletion(store, id, lifeTime)
		fmt.Fprintf(w, "%s/%s\n", siteURL, id)

	default:
		http.Error(w, unknownAction, http.StatusBadRequest)
		return
	}
}

func setupStore(storageType string, args []string) (store Store, err error) {
	params, e := map[string]map[string]string{
		"fs": {
			"dir": "pastes",
		},
		"mmap": {
			"dir": "pastes",
		},
		"mem": {},
	}[storageType]
	if !e {
		return nil, fmt.Errorf("unknown storage type '%s'", storageType)
	}
	if len(args) > len(params) {
		return nil, fmt.Errorf("too many arguments given for %s", storageType)
	}

	for k := range params {
		if len(args) == 0 {
			break
		}
		params[k] = args[0]
		args = args[1:]
	}

	switch storageType {
	case "fs":
		log.Printf("Starting up file store in the directory '%s'", params["dir"])
		return newFileStore(params["dir"])
	case "mmap":
		log.Printf("Starting up mmapped file store in the directory '%s'", params["dir"])
		return newMmapStore(params["dir"])
	case "mem":
		log.Printf("Starting up in-memory store")
		return newMemStore()
	}
	return nil, nil
}

func main() {
	var err error
	flag.Parse()
	templates = template.Must(template.ParseGlob("tmpl/*.html"))

	log.Printf("siteURL    = %s", siteURL)
	log.Printf("listen     = %s", listen)
	log.Printf("lifeTime   = %s", lifeTime)
	log.Printf("maxSize    = %s", maxSize)
	log.Printf("maxNumber  = %d", maxNumber)
	log.Printf("maxStorage = %s", maxStorage)

	args := flag.Args()
	if len(args) == 0 {
		args = []string{"fs"}
	}
	if store, err = setupStore(args[0], args[1:]); err != nil {
		log.Fatalf("Could not setup paste store: %s", err)
	}

	ticker := time.NewTicker(statsReport)
	go func() {
		for _ = range ticker.C {
			log.Println(store.Report())
		}
	}()
	http.HandleFunc("/", handler)
	log.Println("Up and running!")
	log.Println(store.Report())
	log.Fatal(http.ListenAndServe(listen, nil))
}
