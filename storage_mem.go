/* Copyright (c) 2014-2015, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"bytes"
	"sync"
	"time"
)

type MemStore struct {
	sync.RWMutex
	cache map[ID]memCache

	stats Stats
}

type memCache struct {
	buffer  []byte
	modTime time.Time
	size    int64
}

type MemPaste struct {
	content *bytes.Reader
	cache   *memCache
}

func (ps MemPaste) Read(p []byte) (n int, err error) {
	return ps.content.Read(p)
}

func (ps MemPaste) ReadAt(p []byte, off int64) (n int, err error) {
	return ps.content.ReadAt(p, off)
}

func (ps MemPaste) Seek(offset int64, whence int) (i int64, err error) {
	return ps.content.Seek(offset, whence)
}

func (ps MemPaste) Close() error {
	return nil
}

func (ps MemPaste) ModTime() time.Time {
	return ps.cache.modTime
}

func (ps MemPaste) Size() int64 {
	return ps.cache.size
}

func NewMemStore() (s *MemStore, err error) {
	s = new(MemStore)
	s.cache = make(map[ID]memCache)
	return
}

func (s *MemStore) Get(id ID) (Paste, error) {
	s.RLock()
	defer s.RUnlock()
	cached, e := s.cache[id]
	if !e {
		return nil, ErrPasteNotFound
	}
	reader := bytes.NewReader(cached.buffer)
	return MemPaste{content: reader, cache: &cached}, nil
}

func (s *MemStore) Put(content []byte) (ID, error) {
	s.Lock()
	defer s.Unlock()
	size := int64(len(content))
	if err := s.stats.hasSpaceFor(size); err != nil {
		return ID{}, err
	}
	available := func(id ID) bool {
		_, e := s.cache[id]
		return !e
	}
	id, err := randomID(available)
	if err != nil {
		return id, err
	}
	s.stats.makeSpaceFor(size)
	s.cache[id] = memCache{
		buffer:  content,
		modTime: time.Now(),
		size:    size,
	}
	return id, nil
}

func (s *MemStore) Delete(id ID) error {
	s.Lock()
	defer s.Unlock()
	cached, e := s.cache[id]
	if !e {
		return ErrPasteNotFound
	}
	delete(s.cache, id)
	s.stats.freeSpace(cached.size)
	return nil
}

func (s *MemStore) Report() string {
	s.Lock()
	defer s.Unlock()
	return s.stats.Report()
}
