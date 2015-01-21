/* Copyright (c) 2014-2015, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"
)

var (
	ErrPasteNotFound   = errors.New("paste could not be found")
	ErrNoUnusedIDFound = errors.New("gave up trying to find an unused random id")
)

type Store interface {
	// Get the content and header of a paste by its ID and an error, if
	// any.
	Get(id ID) (Content, *Header, error)
	// Put a new paste given its content. Will return the ID assigned to
	// the new paste and an error, if any.
	Put(content []byte) (ID, error)
	// Delete an existing paste by its ID. Will return an error, if any.
	Delete(id ID) error
	Report() string
}

func genHeader(id ID, modTime time.Time, size int64) (h Header) {
	h.ModTime = modTime
	h.Size = size
	if lifeTime > 0 {
		h.Expires = modTime.Add(lifeTime).UTC().Format(http.TimeFormat)
	}
	h.Etag = fmt.Sprintf("%d-%s", h.ModTime.Unix(), id)
	return
}

func randomID(available func(ID) bool) (id ID, err error) {
	for try := 0; try < randTries; try++ {
		if _, err := rand.Read(id[:]); err != nil {
			continue
		}
		if available(id) {
			return id, nil
		}
	}
	return id, ErrNoUnusedIDFound
}

func setupPasteDeletion(store Store, id ID, after time.Duration) {
	if after == 0 {
		return
	}
	timer := time.NewTimer(after)
	go func() {
		for {
			<-timer.C
			if err := store.Delete(id); err == nil {
				break
			}
			log.Printf("Could not delete %s, will try again in %s", id, deleteRetry)
			timer.Reset(deleteRetry)
		}
	}()
}
