/* Copyright (c) 2014-2015, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"crypto/rand"
	"errors"
	"io"
	"log"
	"time"
)

var (
	ErrPasteNotFound   = errors.New("paste could not be found")
	ErrNoUnusedIDFound = errors.New("gave up trying to find an unused random id")
)

// Interface for accessing paste content and information
type Paste interface {
	io.Reader
	io.ReaderAt
	io.Seeker
	io.Closer
	ModTime() time.Time
	Size() int64
}

type Store interface {
	// Get the paste known by the given ID and an error, if any.
	Get(id ID) (Paste, error)

	// Put a new paste given its content. Will return the ID assigned to
	// the new paste and an error, if any.
	Put(content []byte) (ID, error)

	// Delete an existing paste by its ID. Will return an error, if any.
	Delete(id ID) error
}

func randomID(available func(ID) bool) (ID, error) {
	var id ID
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

func setupPasteDeletion(s Store, stats *Stats, id ID, size int64, after time.Duration) {
	if after == 0 {
		return
	}
	timer := time.NewTimer(after)
	go func() {
		for {
			<-timer.C
			if err := s.Delete(id); err == nil {
				stats.freeSpace(size)
				break
			}
			log.Printf("Could not delete %s, will try again in %s", id, deleteRetry)
			timer.Reset(deleteRetry)
		}
	}()
}
