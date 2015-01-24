/* Copyright (c) 2014-2015, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"errors"
	"io"
	"sync"
	"time"
)

type bufferContent struct {
	b []byte
	i int64
}

func (c bufferContent) Read(p []byte) (n int, err error) {
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

func (c bufferContent) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, errors.New("pastecat.bufferContent.ReadAt: negative offset")
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

func (c bufferContent) Seek(offset int64, whence int) (i int64, err error) {
	switch whence {
	case 0:
		i = offset
	case 1:
		i = c.i + offset
	case 2:
		i = int64(len(c.b)) + offset
	default:
		return 0, errors.New("pastecat.bufferContent.Seek: invalid whence")
	}
	if i < 0 {
		return 0, errors.New("pastecat.bufferContent.Seek: negative position")
	}
	c.i = i
	return
}

func (c bufferContent) Close() error {
	return nil
}

type MemStore struct {
	sync.RWMutex
	store map[ID]memCache

	stats Stats
}

type memCache struct {
	header  Header
	content []byte
}

func newMemStore() (s *MemStore, err error) {
	s = new(MemStore)
	s.store = make(map[ID]memCache)
	return
}

func (s *MemStore) Get(id ID) (Content, *Header, error) {
	s.RLock()
	defer s.RUnlock()
	stored, e := s.store[id]
	if !e {
		return nil, nil, ErrPasteNotFound
	}
	bufferContent := bufferContent{b: stored.content}
	return bufferContent, &stored.header, nil
}

func (s *MemStore) Put(content []byte) (ID, error) {
	s.Lock()
	defer s.Unlock()
	size := int64(len(content))
	if err := s.stats.hasSpaceFor(size); err != nil {
		return ID{}, err
	}
	available := func(id ID) bool {
		_, e := s.store[id]
		return !e
	}
	id, err := randomID(available)
	if err != nil {
		return id, err
	}
	s.stats.makeSpaceFor(size)
	s.store[id] = memCache{
		header:  genHeader(id, time.Now(), size),
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

func (s *MemStore) Report() string {
	s.Lock()
	defer s.Unlock()
	return s.stats.Report()
}
