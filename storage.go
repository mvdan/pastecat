/* Copyright (c) 2014, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"
)

var (
	ErrPasteNotFound        = errors.New("paste could not be found")
	ErrNoUnusedIDFound      = errors.New("gave up trying to find an unused random id")
	ErrReachedMax           = errors.New("reached maximum capacity of pastes")
	ErrReachedMaxRecovering = errors.New("reached maximum capacity of pastes while recovering")
)

type Store interface {
	Get(id ID) (Content, *Header, error)
	Put(content []byte) (ID, error)
	Delete(id ID) error
	Report() string
}

type Stats struct {
	number, maxNumber   int
	storage, maxStorage ByteSize
}

func (s *Stats) hasSpaceFor(size ByteSize) bool {
	if s.maxNumber > 0 && s.number >= s.maxNumber {
		return false
	}
	if s.maxStorage > 0 && s.storage+size > s.maxStorage {
		return false
	}
	return true
}

func (s *Stats) makeSpaceFor(size ByteSize) {
	s.number++
	s.storage += size
}

func (s *Stats) freeSpace(size ByteSize) {
	s.number--
	s.storage -= size
}

func (s *Stats) Report() string {
	numberStat := fmt.Sprintf("%d", s.number)
	if s.maxNumber > 0 {
		numberStat += fmt.Sprintf(" (%.2f%% out of %d)",
			float64(s.number*100)/float64(s.maxNumber), s.maxNumber)
	}
	sizeStat := fmt.Sprintf("%s", s.storage)
	if s.maxStorage > 0 {
		sizeStat += fmt.Sprintf(" (%.2f%% out of %s)",
			float64(s.storage*100)/float64(s.maxStorage), s.maxStorage)
	}
	return fmt.Sprintf("Have a total of %s pastes using %s", numberStat, sizeStat)
}

func genHeader(id ID, lifeTime time.Duration, modTime time.Time, size ByteSize) (p Header) {
	p.ModTime = modTime
	p.Size = size
	if lifeTime > 0 {
		p.Expires = modTime.Add(lifeTime).UTC().Format(http.TimeFormat)
	}
	p.Etag = fmt.Sprintf("%d-%s", p.ModTime.Unix(), id)
	return
}

func SetupPasteDeletion(store Store, id ID, lifeTime time.Duration) {
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
			log.Printf("Could not delete %s, will try again in %s", id, deleteRetry)
			timer.Reset(deleteRetry)
		}
	}()
}
