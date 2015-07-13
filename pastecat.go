// Copyright (c) 2014-2015, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/mvdan/pastecat/storage"

	"github.com/mvdan/bytesize"
	"github.com/ogier/pflag"
)

const (
	// Name of the HTTP form field when uploading a paste
	fieldName = "paste"
	// Content-Type when serving pastes
	contentType = "text/plain; charset=utf-8"
	// Report usage stats how often
	reportInterval = 1 * time.Minute

	// HTTP response strings
	invalidID     = "invalid paste id"
	unknownAction = "unsupported action"
)

var (
	siteURL   = pflag.StringP("url", "u", "http://localhost:8080", "URL of the site")
	listen    = pflag.StringP("listen", "l", ":8080", "Host and port to listen to")
	lifeTime  = pflag.DurationP("lifetime", "t", 24*time.Hour, "Lifetime of the pastes")
	timeout   = pflag.DurationP("timeout", "T", 5*time.Second, "Timeout of HTTP requests")
	maxNumber = pflag.IntP("max-number", "m", 0, "Maximum number of pastes to store at once")

	maxSize    = 1 * bytesize.MB
	maxStorage = 1 * bytesize.GB
)

func init() {
	pflag.VarP(&maxSize, "max-size", "s", "Maximum size of pastes")
	pflag.VarP(&maxStorage, "max-storage", "M", "Maximum storage size to use at once")
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

func setHeaders(header http.Header, id storage.ID, paste storage.Paste) {
	modTime := paste.ModTime()
	header.Set("Etag", fmt.Sprintf(`"%d-%s"`, modTime.Unix(), id))
	if *lifeTime > 0 {
		deathTime := modTime.Add(*lifeTime)
		lifeLeft := deathTime.Sub(time.Now())
		header.Set("Expires", deathTime.UTC().Format(http.TimeFormat))
		header.Set("Cache-Control", fmt.Sprintf(
			"max-age=%.f, must-revalidate", lifeLeft.Seconds()))
	}
	header.Set("Content-Type", contentType)
}

type httpHandler struct {
	store storage.Store
	stats *storage.Stats
}

func (h httpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		h.handleGet(w, r)
	case "POST":
		h.handlePost(w, r)
	default:
		http.Error(w, unknownAction, http.StatusBadRequest)
	}
}

func (h *httpHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	if _, e := templates[r.URL.Path]; e {
		err := tmpl.ExecuteTemplate(w, r.URL.Path,
			struct {
				SiteURL   string
				MaxSize   bytesize.ByteSize
				LifeTime  time.Duration
				FieldName string
			}{
				SiteURL:   *siteURL,
				MaxSize:   maxSize,
				LifeTime:  *lifeTime,
				FieldName: fieldName,
			})
		if err != nil {
			log.Printf("Error executing template for %s: %s", r.URL.Path, err)
		}
		return
	}
	id, err := storage.IDFromString(r.URL.Path[1:])
	if err != nil {
		http.Error(w, invalidID, http.StatusBadRequest)
		return
	}
	paste, err := h.store.Get(id)
	if err == storage.ErrPasteNotFound {
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
}

func (h *httpHandler) handlePost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxSize))
	content, err := getContentFromForm(r)
	size := int64(len(content))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.stats.MakeSpaceFor(size); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	}
	id, err := h.store.Put(content)
	if err != nil {
		log.Printf("Unknown error on POST: %s", err)
		h.stats.FreeSpace(size)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	storage.SetupPasteDeletion(h.store, h.stats, id, size, *lifeTime)
	fmt.Fprintf(w, "%s/%s\n", *siteURL, id)
}

func (h *httpHandler) setupStore(lifeTime time.Duration, storageType string, args []string) error {
	params, e := map[string]map[string]string{
		"fs": {
			"dir": "pastes",
		},
		"fs-mmap": {
			"dir": "pastes",
		},
		"mem": {},
	}[storageType]
	if !e {
		return fmt.Errorf("unknown storage type '%s'", storageType)
	}
	if len(args) > len(params) {
		return fmt.Errorf("too many arguments given for %s", storageType)
	}
	for k := range params {
		if len(args) == 0 {
			break
		}
		params[k] = args[0]
		args = args[1:]
	}
	var err error
	switch storageType {
	case "fs":
		log.Printf("Starting up file store in the directory '%s'", params["dir"])
		h.store, err = storage.NewFileStore(h.stats, lifeTime, params["dir"])
	case "fs-mmap":
		log.Printf("Starting up mmapped file store in the directory '%s'", params["dir"])
		h.store, err = storage.NewMmapStore(h.stats, lifeTime, params["dir"])
	case "mem":
		log.Printf("Starting up in-memory store")
		h.store, err = storage.NewMemStore()
	}
	return err
}

func logStats(stats *storage.Stats) {
	num, stg := stats.Report()
	var numStats, stgStats string
	if stats.MaxNumber > 0 {
		numStats = fmt.Sprintf("%d (%.2f%% out of %d)", num,
			float64(num*100)/float64(stats.MaxNumber), stats.MaxNumber)
	} else {
		numStats = fmt.Sprintf("%d", num)
	}
	if stats.MaxStorage > 0 {
		stgStats = fmt.Sprintf("%s (%.2f%% out of %s)", bytesize.ByteSize(stg),
			float64(stg*100)/float64(stats.MaxStorage), bytesize.ByteSize(stats.MaxStorage))
	} else {
		stgStats = fmt.Sprintf("%s", bytesize.ByteSize(stg))
	}
	log.Printf("Have a total of %s pastes using %s", numStats, stgStats)
}

func main() {
	pflag.Parse()
	if maxStorage > 1*bytesize.EB {
		log.Fatalf("Specified a maximum storage size that would overflow int64!")
	}
	if maxSize > 1*bytesize.EB {
		log.Fatalf("Specified a maximum paste size that would overflow int64!")
	}
	loadTemplates()
	var handler httpHandler
	handler.stats = &storage.Stats{
		MaxNumber:  *maxNumber,
		MaxStorage: int64(maxStorage),
	}
	log.Printf("siteURL    = %s", *siteURL)
	log.Printf("listen     = %s", *listen)
	log.Printf("lifeTime   = %s", *lifeTime)
	log.Printf("maxSize    = %s", maxSize)
	log.Printf("maxNumber  = %d", *maxNumber)
	log.Printf("maxStorage = %s", maxStorage)

	args := pflag.Args()
	if len(args) == 0 {
		args = []string{"fs"}
	}
	if err := handler.setupStore(*lifeTime, args[0], args[1:]); err != nil {
		log.Fatalf("Could not setup paste store: %s", err)
	}

	ticker := time.NewTicker(reportInterval)
	go func() {
		logStats(handler.stats)
		for range ticker.C {
			logStats(handler.stats)
		}
	}()
	var finalHandler http.Handler = handler
	if *timeout > 0 {
		finalHandler = http.TimeoutHandler(finalHandler, *timeout, "")
	}
	http.Handle("/", finalHandler)
	log.Println("Up and running!")
	log.Fatal(http.ListenAndServe(*listen, nil))
}
