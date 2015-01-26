/* Copyright (c) 2014-2015, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"html/template"
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

	maxSize    = 1 * bytesize.MB
	maxStorage = 1 * bytesize.GB

	templates *template.Template
	startTime = time.Now()

	store Store
	stats Stats
)

func init() {
	flag.StringVar(&siteURL, "u", "http://localhost:8080", "URL of the site")
	flag.StringVar(&listen, "l", ":8080", "Host and port to listen to")
	flag.DurationVar(&lifeTime, "t", 24*time.Hour, "Lifetime of the pastes")
	flag.IntVar(&stats.maxNumber, "m", 0, "Maximum number of pastes to store at once")
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

func getContentFromForm(r *http.Request) ([]byte, error) {
	if value := r.FormValue(fieldName); len(value) > 0 {
		return []byte(value), nil
	}
	if f, _, err := r.FormFile(fieldName); err == nil {
		defer f.Close()
		content, err := ioutil.ReadAll(f)
		if err == nil && len(content) > 0 {
			return content, nil
		}
	}
	return nil, errors.New("no paste provided")
}

func setHeaders(header http.Header, id ID, paste Paste) {
	modTime := paste.ModTime()
	header.Set("Etag", fmt.Sprintf("%d-%s", modTime.Unix(), id))
	if lifeTime > 0 {
		deathTime := modTime.Add(lifeTime)
		lifeLeft := deathTime.Sub(time.Now())
		header.Set("Expires", deathTime.UTC().Format(http.TimeFormat))
		header.Set("Cache-Control", fmt.Sprintf(
			"max-age=%.f, must-revalidate", lifeLeft.Seconds()))
	}
	header.Set("Content-Type", contentType)
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
		paste, err := store.Get(id)
		if err == ErrPasteNotFound {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		} else if err != nil {
			log.Printf("Unknown error on GET: %s", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer paste.Close()
		setHeaders(w.Header(), id, paste)
		http.ServeContent(w, r, "", paste.ModTime(), paste)

	case "POST":
		r.Body = http.MaxBytesReader(w, r.Body, int64(maxSize))
		content, err := getContentFromForm(r)
		size := int64(len(content))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := stats.makeSpaceFor(size); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
		}
		id, err := store.Put(content)
		if err != nil {
			log.Printf("Unknown error on POST: %s", err)
			stats.freeSpace(size)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		setupPasteDeletion(store, id, size, lifeTime)
		fmt.Fprintf(w, "%s/%s\n", siteURL, id)

	default:
		http.Error(w, unknownAction, http.StatusBadRequest)
		return
	}
}

func setupStore(storageType string, args []string) (Store, error) {
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
		return NewFileStore(params["dir"])
	case "mmap":
		log.Printf("Starting up mmapped file store in the directory '%s'", params["dir"])
		return NewMmapStore(params["dir"])
	case "mem":
		log.Printf("Starting up in-memory store")
		return NewMemStore()
	}
	return nil, nil
}

func main() {
	var err error
	flag.Parse()
	templates = template.Must(template.ParseGlob("tmpl/*.html"))

	if maxStorage > 1*bytesize.EB {
		log.Fatalf("Specified a maximum storage size that would overflow int64!")
	}
	stats.maxStorage = int64(maxStorage)

	log.Printf("siteURL    = %s", siteURL)
	log.Printf("listen     = %s", listen)
	log.Printf("lifeTime   = %s", lifeTime)
	log.Printf("maxSize    = %s", maxSize)
	log.Printf("maxNumber  = %d", stats.maxNumber)
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
			log.Println(stats.Report())
		}
	}()
	http.HandleFunc("/", handler)
	log.Println("Up and running!")
	log.Println(stats.Report())
	log.Fatal(http.ListenAndServe(listen, nil))
}
