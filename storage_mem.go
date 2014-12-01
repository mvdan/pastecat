/* Copyright (c) 2014, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"crypto/rand"
	"errors"
	"io"
	"sync"
	"time"
)

type BufferContent struct {
	b []byte
	i int64
}

func (c BufferContent) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	if c.i >= int64(len(c.b)) {
		return 0, io.EOF
	}
	n = copy(p, c.b[c.i:])
	c.i += int64(n)
	return
}

func (c BufferContent) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, errors.New("pastecat.BufferedPasteContent.ReadAt: negative offset")
	}
	if off >= int64(len(c.b)) {
		return 0, io.EOF
	}
	n = copy(p, c.b[off:])
	if n < len(p) {
		err = io.EOF
	}
	return
}

func (c BufferContent) Seek(offset int64, whence int) (i int64, err error) {
	switch whence {
	case 0:
		i = offset
	case 1:
		i = c.i + offset
	case 2:
		i = int64(len(c.b)) + offset
	default:
		return 0, errors.New("pastecat.BufferedPasteContent.Seek: invalid whence")
	}
	if i < 0 {
		return 0, errors.New("pastecat.BufferedPasteContent.Seek: negative position")
	}
	c.i = i
	return
}

func (c BufferContent) Close() error {
	return nil
}

type MemStore struct {
	sync.RWMutex
	store map[ID]memCache

	lifeTime time.Duration
	stats    Stats
}

type memCache struct {
	header  Header
	content []byte
}

func newMemStore(maxNumber int, maxStorage ByteSize, lifeTime time.Duration) (s *MemStore, err error) {
	s = new(MemStore)
	s.lifeTime = lifeTime
	s.store = make(map[ID]memCache)
	s.stats = Stats{maxNumber: maxNumber, maxStorage: maxStorage}
	return
}

func (s *MemStore) Get(id ID) (Content, *Header, error) {
	s.RLock()
	defer s.RUnlock()
	stored, e := s.store[id]
	if !e {
		return nil, nil, ErrPasteNotFound
	}
	bufferContent := BufferContent{b: stored.content}
	return bufferContent, &stored.header, nil
}

func (s *MemStore) Put(content []byte) (id ID, err error) {
	s.Lock()
	defer s.Unlock()
	size := ByteSize(len(content))
	if !s.stats.hasSpaceFor(size) {
		return id, ErrReachedMax
	}
	if id, err = s.randomID(); err != nil {
		return
	}
	s.stats.makeSpaceFor(size)
	s.store[id] = memCache{
		header:  genHeader(id, s.lifeTime, time.Now(), size),
		content: content,
	}
	return id, nil
}

func (s *MemStore) Delete(id ID) error {
	s.Lock()
	defer s.Unlock()
	stored, e := s.store[id]
	if !e {
		return ErrPasteNotFound
	}
	delete(s.store, id)
	s.stats.freeSpace(stored.header.Size)
	return nil
}

func (s *MemStore) randomID() (id ID, err error) {
	for try := 0; try < randTries; try++ {
		if _, err := rand.Read(id[:]); err != nil {
			continue
		}
		if _, e := s.store[id]; !e {
			return id, nil
		}
	}
	return id, ErrNoUnusedIDFound
}

func (s *MemStore) Report() string {
	s.Lock()
	defer s.Unlock()
	return s.stats.Report()
}
