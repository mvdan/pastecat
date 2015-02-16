/* Copyright (c) 2014-2015, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package storage

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"time"
)

const (
	// Length of the random hexadecimal ids assigned to pastes. At least 4.
	idSize = 8
	// Number of times to try getting an unused random paste id
	randTries = 10
	// Number of times times to retry deleting a paste
	deleteRetries = 5
	// How long to wait before retrying to delete a paste
	deleteRetryTimeout = 1 * time.Minute
)

var (
	// ErrPasteNotFound means that we could not find the requested paste
	ErrPasteNotFound = errors.New("paste could not be found")
	// ErrNoUnusedIDFound means that we could not find an unused ID to
	// allocate to a new paste
	ErrNoUnusedIDFound = errors.New("gave up trying to find an unused random id")
)

// A Paste represents the paste's content and information
type Paste interface {
	io.Reader
	io.ReaderAt
	io.Seeker
	io.Closer
	ModTime() time.Time
	Size() int64
}

// ID is the binary representation of the identifier for a paste
type ID [idSize / 2]byte

// IDFromString parses a hexadecimal string into an ID. Returns the ID and an
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

// A Store represents a database holding multiple pastes identified by their
// ids
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

func SetupPasteDeletion(s Store, stats *Stats, id ID, size int64, after time.Duration) {
	if after == 0 {
		return
	}
	f := func() {
		del := func() error {
			if err := s.Delete(id); err != nil {
				return err
			}
			stats.FreeSpace(size)
			return nil
		}
		if err := del(); err == nil {
			return
		}
		timer := time.NewTimer(deleteRetryTimeout)
		for i := 0; i < deleteRetries; i++ {
			log.Printf("Could not delete %s, trying again in %s", id, deleteRetryTimeout)
			<-timer.C
			if err := del(); err == nil {
				break
			}
			timer.Reset(deleteRetryTimeout)
		}
		log.Printf("Giving up on deleting %s", id)
	}
	time.AfterFunc(after, f)
}
