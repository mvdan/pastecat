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
	"sync"
	"time"
)

const (
	idSize      = 8 // At least 4
	rawIdSize   = idSize / 2
	randTries   = 10
	fieldName   = "paste"
	contentType = "text/plain; charset=utf-8"

	// GET error messages
	invalidId     = "Invalid paste id."
	pasteNotFound = "Paste doesn't exist."

	// POST error messages
	reachedMax = "Reached maximum capacity of pastes. Please try again later."

	// Common error messages
	unknownError = "Something went terribly wrong."
	timedOut     = "Request timed out."
)

var (
	siteUrl, listen, dataDir  string
	lifeTime, timeout         time.Duration
	maxNumber                 int
	maxSizeStr, maxStorageStr string
	maxSize, maxStorage       byteSize
	templates                 *template.Template

	regexByteSize = regexp.MustCompile(`^([\d\.]+)\s*([KMGT]?B?)$`)
	startTime     = time.Now()
)

func init() {
	flag.StringVar(&siteUrl, "u", "http://localhost:8080", "URL of the site")
	flag.StringVar(&listen, "l", ":8080", "Host and port to listen to")
	flag.StringVar(&dataDir, "d", "data", "Directory to store all the pastes in")
	flag.DurationVar(&lifeTime, "t", 12*time.Hour, "Lifetime of the pastes")
	flag.StringVar(&maxSizeStr, "s", "1M", "Maximum size of pastes")
	flag.IntVar(&maxNumber, "m", 0, "Maximum number of pastes to store at once")
	flag.StringVar(&maxStorageStr, "M", "1G", "Maximum storage size to use at once")
	flag.DurationVar(&timeout, "T", 200*time.Millisecond, "Timeout of requests")
}

type byteSize int64

const (
	_ byteSize = 1 << (10 * iota)
	kbyte
	mbyte
	gbyte
	tbyte
)

func parseByteSize(str string) (byteSize, error) {
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
	return byteSize(size), nil
}

func (b byteSize) String() string {
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

type Id [rawIdSize]byte

func IdFromString(hexId string) (id Id, err error) {
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

type PasteInfo struct {
	Etag, Path, Expires string
	ModTime             time.Time
}

func (id Id) genPasteInfo(modTime time.Time) (pasteInfo PasteInfo) {
	pasteInfo.ModTime = modTime
	if lifeTime > 0 {
		pasteInfo.Expires = modTime.Add(lifeTime).UTC().Format(http.TimeFormat)
	}
	pasteInfo.Etag = fmt.Sprintf("%d-%s", pasteInfo.ModTime.Unix(), id)
	hexId := id.String()
	pasteInfo.Path = path.Join(hexId[0:2], hexId[2:])
	return
}

type statsWorker struct {
	number int
	size   byteSize
	inc    chan byteSize
	dec    chan byteSize
	ret    chan bool
	report chan struct{}
}

var stats statsWorker

func (s statsWorker) work() {
	for {
		select {
		case size := <-s.inc:
			if maxNumber > 0 && s.number >= maxNumber {
				s.ret <- false
			} else if maxStorage > 0 && s.size+size > maxStorage {
				s.ret <- false
			} else {
				s.number++
				s.size += size
				s.ret <- true
			}
		case size := <-s.dec:
			s.number--
			s.size -= size
		case <-s.report:
			numberStat := fmt.Sprintf("%d", s.number)
			if maxNumber > 0 {
				numberStat += fmt.Sprintf(" (%.2f%% out of %d)",
					float64(s.number*100)/float64(maxNumber), maxNumber)
			}
			sizeStat := fmt.Sprintf("%s", s.size)
			if maxStorage > 0 {
				sizeStat += fmt.Sprintf(" (%.2f%% out of %s)",
					float64(s.size*100)/float64(maxStorage), maxStorage)
			}
			log.Printf("Have a total of %s pastes using %s", numberStat, sizeStat)
		}
	}
}

func (s statsWorker) reporter() {
	recovering.Wait()
	log.Println("Finished recovering all pastes from the data directory.")
	s.report <- struct{}{}
	ticker := time.NewTicker(time.Minute)
	for _ = range ticker.C {
		s.report <- struct{}{}
	}
}

type getRequest struct {
	w  http.ResponseWriter
	r  *http.Request
	id Id
}

type postRequest struct {
	w       http.ResponseWriter
	done    chan struct{}
	content []byte
	modTime time.Time
}

type worker struct {
	num  byte
	get  chan getRequest
	del  chan Id
	done chan struct{}
	m    map[Id]PasteInfo
}

var workers [256]worker
var post = make(chan postRequest) // Posting is shared to balance load
var recovering sync.WaitGroup

func (w worker) recoverPaste(filePath string, fileInfo os.FileInfo, err error) error {
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
	id, err := IdFromString(hexId)
	if err != nil {
		return err
	}
	modTime := fileInfo.ModTime()
	deathTime := modTime.Add(lifeTime)
	if deathTime.Before(startTime) {
		return os.Remove(filePath)
	}
	if modTime.After(startTime) {
		modTime = startTime
	}
	stats.inc <- byteSize(fileInfo.Size())
	if !<-stats.ret {
		return errors.New("Reached maximum capacity of pastes while recovering " + filePath)
	}
	w.m[id] = id.genPasteInfo(modTime)
	w.DeletePasteAfter(id, deathTime.Sub(startTime))
	return nil
}

func (w worker) RandomId() (id Id, err error) {
	id[0] = w.num
	for try := 0; try < randTries; try++ {
		if _, err := rand.Read(id[1:]); err != nil {
			continue
		}
		if _, e := w.m[id]; !e {
			return id, nil
		}
	}
	return id, fmt.Errorf("Gave up trying to find an unused random id after %d tries", randTries)
}

func (w worker) work() {
	dir := hex.EncodeToString([]byte{w.num})
	if stat, err := os.Stat(dir); err == nil {
		if !stat.IsDir() {
			log.Fatalf("%s/%s exists but is not a directory!", dataDir, dir)
		}
	} else if err := os.Mkdir(dir, 0700); err != nil {
		log.Fatalf("Could not create data directory %s/%s: %s", dataDir, dir, err)
	}
	if err := filepath.Walk(dir, w.recoverPaste); err != nil {
		log.Fatalf("Could not recover data directory %s/%s: %s", dataDir, dir, err)
	}
	recovering.Done()
	for {
		var done chan struct{}
		select {
		case request := <-w.get:
			done = w.done
			pasteInfo, e := w.m[request.id]
			if !e {
				http.Error(request.w, pasteNotFound, http.StatusNotFound)
				break
			}
			pasteFile, err := os.Open(pasteInfo.Path)
			if err != nil {
				http.Error(request.w, unknownError, http.StatusInternalServerError)
				break
			}
			request.w.Header().Set("Etag", pasteInfo.Etag)
			if lifeTime > 0 {
				request.w.Header().Set("Expires", pasteInfo.Expires)
			}
			request.w.Header().Set("Content-Type", contentType)
			http.ServeContent(request.w, request.r, "", pasteInfo.ModTime, pasteFile)
			pasteFile.Close()

		case request := <-post:
			done = request.done
			pasteSize := byteSize(len(request.content))
			stats.inc <- pasteSize
			if !<-stats.ret {
				http.Error(request.w, reachedMax, http.StatusServiceUnavailable)
				break
			}
			id, err := w.RandomId()
			if err != nil {
				log.Println(err)
				http.Error(request.w, unknownError, http.StatusInternalServerError)
				break
			}
			pasteInfo := id.genPasteInfo(request.modTime)
			pasteFile, err := os.OpenFile(pasteInfo.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				log.Printf("Could not create new paste file %s: %s", pasteInfo.Path, err)
				http.Error(request.w, unknownError, http.StatusInternalServerError)
				stats.dec <- pasteSize
				break
			}
			_, err = pasteFile.Write(request.content)
			pasteFile.Close()
			if err != nil {
				log.Printf("Could not write data into %s: %s", pasteInfo.Path, err)
				http.Error(request.w, unknownError, http.StatusInternalServerError)
				stats.dec <- pasteSize
				break
			}
			w.m[id] = pasteInfo
			if lifeTime > 0 {
				w.DeletePasteAfter(id, lifeTime)
			}
			fmt.Fprintf(request.w, "%s/%s\n", siteUrl, id)

		case id := <-w.del:
			pasteInfo, _ := w.m[id]
			var pasteSize byteSize
			if fileInfo, err := os.Lstat(pasteInfo.Path); err == nil {
				pasteSize = byteSize(fileInfo.Size())
			} else {
				log.Printf("Could not stat paste to be removed %s: %s", id, err)
				w.DeletePasteAfter(id, 2*time.Minute)
				break
			}
			if err := os.Remove(pasteInfo.Path); err == nil {
				stats.dec <- pasteSize
				delete(w.m, id)
			} else {
				log.Printf("Could not remove %s: %s", id, err)
				w.DeletePasteAfter(id, 2*time.Minute)
				break
			}
		}
		if done != nil {
			done <- struct{}{}
		}
	}
}

func (w worker) DeletePasteAfter(id Id, duration time.Duration) {
	timer := time.NewTimer(duration)
	go func() {
		<-timer.C
		w.del <- id
	}()
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
		return strings.Join(limits, " ") + "\n\n";
	}
	return ""
}

func handler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		switch r.URL.Path {
		case "/":
			templates.ExecuteTemplate(w, "index.html",
				struct{ SiteUrl, LimitDesc, FieldName string }{
					siteUrl, describeLimits(), fieldName})
			return
		case "/form":
			templates.ExecuteTemplate(w, "form.html",
				struct{ SiteUrl, LimitDesc, FieldName string }{
					siteUrl, describeLimits(), fieldName})
			return
		}
		id, err := IdFromString(r.URL.Path[1:])
		if err != nil {
			http.Error(w, invalidId, http.StatusBadRequest)
			return
		}
		var timer *time.Timer
		if timeout > 0 {
			timer = time.NewTimer(timeout)
		}
		worker := workers[id[0]]
		select {
		case <-timer.C:
			http.Error(w, timedOut, http.StatusRequestTimeout)
		case worker.get <- getRequest{id: id, w: w, r: r}:
			timer.Stop()
		}
		<-worker.done

	case "POST":
		r.Body = http.MaxBytesReader(w, r.Body, int64(maxSize))
		var content []byte
		if value := r.FormValue(fieldName); value != "" {
			content = []byte(value)
		} else if f, _, err := r.FormFile(fieldName); err == nil {
			content, err = ioutil.ReadAll(f)
			f.Close()
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		done := make(chan struct{})
		var timer *time.Timer
		if timeout > 0 {
			timer = time.NewTimer(timeout)
		}
		select {
		case <-timer.C:
			http.Error(w, timedOut, http.StatusRequestTimeout)
		case post <- postRequest{content: content, modTime: time.Now(), w: w, done: done}:
			timer.Stop()
		}
		<-done

	default:
		http.Error(w, "Unsupported action.", http.StatusBadRequest)
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
	if err = os.MkdirAll(dataDir, 0700); err != nil {
		log.Fatalf("Could not create data directory %s: %s", dataDir, err)
	}
	if err = os.Chdir(dataDir); err != nil {
		log.Fatalf("Could not enter data directory %s: %s", dataDir, err)
	}
	log.Printf("siteUrl    = %s", siteUrl)
	log.Printf("listen     = %s", listen)
	log.Printf("dataDir    = %s", dataDir)
	log.Printf("lifeTime   = %s", lifeTime)
	log.Printf("timeout    = %s", timeout)
	log.Printf("maxSize    = %s", maxSize)
	log.Printf("maxNumber  = %d", maxNumber)
	log.Printf("maxStorage = %s", maxStorage)
	stats.inc = make(chan byteSize)
	stats.dec = make(chan byteSize)
	stats.ret = make(chan bool)
	stats.report = make(chan struct{})
	go stats.work()
	for n := range workers {
		w := &workers[n]
		w.num = byte(n)
		w.m = make(map[Id]PasteInfo)
		w.get = make(chan getRequest)
		w.del = make(chan Id)
		w.done = make(chan struct{})
		recovering.Add(1)
		go w.work()
	}
	go stats.reporter()
	http.HandleFunc("/", handler)
	log.Println("Up and running!")
	log.Fatal(http.ListenAndServe(listen, nil))
}
