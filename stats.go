/* Copyright (c) 2014-2015, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"errors"
	"fmt"

	"github.com/mvdan/bytesize"
)

var (
	ErrReachedMax           = errors.New("reached maximum capacity of pastes")
	ErrReachedMaxRecovering = errors.New("reached maximum capacity of pastes while recovering")
)

type Stats struct {
	number  int
	storage int64
}

func (s *Stats) hasSpaceFor(size int64) bool {
	if maxNumber > 0 && s.number >= maxNumber {
		return false
	}
	if maxStorage > 0 && s.storage+size > int64(maxStorage) {
		return false
	}
	return true
}

func (s *Stats) makeSpaceFor(size int64) {
	s.number++
	s.storage += size
}

func (s *Stats) freeSpace(size int64) {
	s.number--
	s.storage -= size
}

func (s *Stats) Report() string {
	numberStat := fmt.Sprintf("%d", s.number)
	if maxNumber > 0 {
		numberStat += fmt.Sprintf(" (%.2f%% out of %d)",
			float64(s.number*100)/float64(maxNumber), maxNumber)
	}
	sizeStat := fmt.Sprintf("%s", bytesize.ByteSize(s.storage))
	if maxStorage > 0 {
		sizeStat += fmt.Sprintf(" (%.2f%% out of %s)",
			float64(s.storage*100)/float64(maxStorage), maxStorage)
	}
	return fmt.Sprintf("Have a total of %s pastes using %s", numberStat, sizeStat)
}
