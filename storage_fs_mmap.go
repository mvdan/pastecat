/* Copyright (c) 2014-2015, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"bytes"
	"os"
	"sync"
	"time"

	memmap "github.com/edsrzf/mmap-go"
)

type MmapStore struct {
	sync.RWMutex
	cache map[ID]mmapCache
	dir   string
}

type mmapCache struct {
	reading sync.WaitGroup
	modTime time.Time
	path    string
	mmap    memmap.MMap
	size    int64
}

type MmapPaste struct {
	content *bytes.Reader
	cache   *mmapCache
}

func (c MmapPaste) Read(p []byte) (n int, err error) {
	return c.content.Read(p)
}

func (c MmapPaste) ReadAt(p []byte, off int64) (n int, err error) {
	return c.content.ReadAt(p, off)
}

func (c MmapPaste) Seek(offset int64, whence int) (int64, error) {
	return c.content.Seek(offset, whence)
}

func (c MmapPaste) Close() error {
	c.cache.reading.Done()
	return nil
}

func (c MmapPaste) ModTime() time.Time {
	return c.cache.modTime
}

func (c MmapPaste) Size() int64 {
	return c.cache.size
}


func NewMmapStore(stats *Stats, lifeTime time.Duration, dir string) (*MmapStore, error) {
	if err := setupTopDir(dir); err != nil {
		return nil, err
	}
	s := new(MmapStore)
	s.dir = dir
	s.cache = make(map[ID]mmapCache)

	insert := func(id ID, path string, modTime time.Time, size int64) error {
		f, err := os.Open(path)
		defer f.Close()
		mmap, err := getMmap(f)
		if err != nil {
			return err
		}
		cached := mmapCache{
			modTime: modTime,
			path:    path,
			mmap:    mmap,
			size:    size,
		}
		s.cache[id] = cached
		return nil
	}
	if err := setupSubdirs(s.dir, fileRecover(insert, s, stats, lifeTime)); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *MmapStore) Get(id ID) (Paste, error) {
	s.RLock()
	defer s.RUnlock()
	cached, e := s.cache[id]
	if !e {
		return nil, ErrPasteNotFound
	}
	reader := bytes.NewReader(cached.mmap)
	cached.reading.Add(1)
	return MmapPaste{content: reader, cache: &cached}, nil
}

func (s *MmapStore) Put(content []byte) (ID, error) {
	size := int64(len(content))
	available := func(id ID) bool {
		_, e := s.cache[id]
		return !e
	}
	s.Lock()
	defer s.Unlock()
	id, err := randomID(available)
	if err != nil {
		return id, err
	}
	path := pathFromID(id)
	if err = writeNewFile(path, content); err != nil {
		return id, err
	}
	f, err := os.Open(path)
	mmap, err := getMmap(f)
	if err != nil {
		return id, err
	}
	s.cache[id] = mmapCache{
		path:    path,
		modTime: time.Now(),
		size:    size,
		mmap:    mmap,
	}
	return id, nil
}

func (s *MmapStore) Delete(id ID) error {
	s.Lock()
	defer s.Unlock()
	cached, e := s.cache[id]
	if !e {
		return ErrPasteNotFound
	}
	delete(s.cache, id)
	cached.reading.Wait()
	if err := cached.mmap.Unmap(); err != nil {
		return err
	}
	if err := os.Remove(cached.path); err != nil {
		return err
	}
	return nil
}

func getMmap(f *os.File) (memmap.MMap, error) {
	return memmap.Map(f, memmap.RDONLY, 0)
}
