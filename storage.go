/* Copyright (c) 2014-2015, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"crypto/rand"
	"errors"
	"io"
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
