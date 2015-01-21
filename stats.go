/* Copyright (c) 2014-2015, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"errors"
	"fmt"

	"github.com/mvdan/bytesize"
)

var (
	ErrReachedMaxNumber  = errors.New("reached maximum number of pastes")
	ErrReachedMaxStorage = errors.New("reached maximum storage of pastes")
)

type Stats struct {
	number  int
	storage int64
}

func (s *Stats) hasSpaceFor(size int64) error {
	if maxNumber > 0 && s.number >= maxNumber {
		return ErrReachedMaxNumber
	}
	if maxStorage > 0 && s.storage+size > int64(maxStorage) {
		return ErrReachedMaxStorage
	}
	return nil
}

func (s *Stats) makeSpaceFor(size int64) {
	s.number++
	s.storage += size
}

func (s *Stats) freeSpace(size int64) {
	s.number--
	s.storage -= size
}

func (s *Stats) reportNumber() string {
	if maxNumber > 0 {
		return fmt.Sprintf("%d (%.2f%% out of %d)", s.number,
			float64(s.number*100)/float64(maxNumber), maxNumber)
	}
	return fmt.Sprintf("%d", s.number)
}

func (s *Stats) reportStorage() string {
	if maxStorage > 0 {
		return fmt.Sprintf("%s (%.2f%% out of %s)", bytesize.ByteSize(s.storage),
			float64(s.storage*100)/float64(maxStorage), maxStorage)
	}
	return fmt.Sprintf("%s", bytesize.ByteSize(s.storage))
}

func (s *Stats) Report() string {
	return fmt.Sprintf("Have a total of %s pastes using %s",
		s.reportNumber(), s.reportStorage())
}
