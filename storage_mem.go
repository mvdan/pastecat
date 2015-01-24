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
	content bufferContent
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

func newMemStore() (s *MemStore, err error) {
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
	content := bufferContent{b: cached.buffer}
	return MemPaste{content: content, cache: &cached}, nil
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
