// Copyright (c) 2014-2015, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package storage

import (
	"errors"
	"sync"
)

var (
	// ErrReachedMaxNumber means that we reached the maximum number of pastes
	ErrReachedMaxNumber = errors.New("reached maximum number of pastes")
	// ErrReachedMaxStorage means that we reached the maximum storage
	// allocated for pastes
	ErrReachedMaxStorage = errors.New("reached maximum storage of pastes")
)

type Stats struct {
	number, MaxNumber   int
	storage, MaxStorage int64
	sync.RWMutex
}

func (s *Stats) MakeSpaceFor(size int64) error {
	s.Lock()
	defer s.Unlock()
	if s.MaxNumber > 0 && s.number >= s.MaxNumber {
		return ErrReachedMaxNumber
	}
	if s.MaxStorage > 0 && s.storage+size > s.MaxStorage {
		return ErrReachedMaxStorage
	}
	s.number++
	s.storage += size
	return nil
}

func (s *Stats) FreeSpace(size int64) {
	s.Lock()
	s.number--
	s.storage -= size
	s.Unlock()
}

func (s *Stats) Report() (int, int64) {
	s.RLock()
	number := s.number
	storage := s.storage
	s.RUnlock()
	return number, storage
}
