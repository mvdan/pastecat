/* Copyright (c) 2014-2015, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"errors"
	"fmt"
	"sync"

	"github.com/mvdan/bytesize"
)

var (
	// ErrReachedMaxNumber means that we reached the maximum number of pastes
	ErrReachedMaxNumber  = errors.New("reached maximum number of pastes")
	// ErrReachedMaxStorage means that we reached the maximum storage
	// allocated for pastes
	ErrReachedMaxStorage = errors.New("reached maximum storage of pastes")
)

type Stats struct {
	number, maxNumber   int
	storage, maxStorage int64
	sync.RWMutex
}

func (s *Stats) makeSpaceFor(size int64) error {
	s.Lock()
	defer s.Unlock()
	if s.maxNumber > 0 && s.number >= s.maxNumber {
		return ErrReachedMaxNumber
	}
	if s.maxStorage > 0 && s.storage+size > s.maxStorage {
		return ErrReachedMaxStorage
	}
	s.number++
	s.storage += size
	return nil
}

func (s *Stats) freeSpace(size int64) {
	s.Lock()
	s.number--
	s.storage -= size
	s.Unlock()
}

func (s *Stats) reportNumber() string {
	if s.maxNumber > 0 {
		return fmt.Sprintf("%d (%.2f%% out of %d)", s.number,
			float64(s.number*100)/float64(s.maxNumber), s.maxNumber)
	}
	return fmt.Sprintf("%d", s.number)
}

func (s *Stats) reportStorage() string {
	if s.maxStorage > 0 {
		return fmt.Sprintf("%s (%.2f%% out of %s)", bytesize.ByteSize(s.storage),
			float64(s.storage*100)/float64(s.maxStorage), bytesize.ByteSize(s.maxStorage))
	}
	return fmt.Sprintf("%s", bytesize.ByteSize(s.storage))
}

func (s *Stats) Report() string {
	s.RLock()
	number := s.reportNumber()
	storage := s.reportStorage()
	s.RUnlock()
	return fmt.Sprintf("Have a total of %s pastes using %s", number, storage)
}
