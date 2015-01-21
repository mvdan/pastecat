/* Copyright (c) 2014-2015, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"os"
	"sync"
	"syscall"
	"time"
)

type MmapStore struct {
	sync.RWMutex
	cache map[ID]mmapCache

	dir   string
	stats Stats
}

type mmapCache struct {
	reading sync.WaitGroup
	header  Header
	path    string
	mmap    []byte
}

type mmapContent struct {
	content bufferContent
	reading *sync.WaitGroup
}

func (c mmapContent) Read(p []byte) (n int, err error) {
	return c.content.Read(p)
}

func (c mmapContent) ReadAt(p []byte, off int64) (n int, err error) {
	return c.content.ReadAt(p, off)
}

func (c mmapContent) Seek(offset int64, whence int) (int64, error) {
	return c.content.Seek(offset, whence)
}

func (c mmapContent) Close() error {
	c.reading.Done()
	return nil
}

func newMmapStore(dir string) (s *MmapStore, err error) {
	setupTopDir(dir)
	s = new(MmapStore)
	s.dir = dir
	s.cache = make(map[ID]mmapCache)
	setupSubdirs(s.dir, s.Recover)
	return
}

func (s *MmapStore) Get(id ID) (Content, *Header, error) {
	s.RLock()
	defer s.RUnlock()
	cached, e := s.cache[id]
	if !e {
		return nil, nil, ErrPasteNotFound
	}
	content := bufferContent{b: cached.mmap}
	cached.reading.Add(1)
	return mmapContent{content, &cached.reading}, &cached.header, nil
}

func (s *MmapStore) Put(content []byte) (id ID, err error) {
	s.Lock()
	defer s.Unlock()
	size := int64(len(content))
	if !s.stats.hasSpaceFor(size) {
		return id, ErrReachedMax
	}
	available := func(id ID) bool {
		_, e := s.cache[id]
		return !e
	}
	if id, err = randomID(available); err != nil {
		return
	}
	pastePath := pathFromID(id)
	if err = writeNewFile(pastePath, content); err != nil {
		return
	}
	f, err := os.Open(pastePath)
	data, err := getMmap(f, len(content))
	if err != nil {
		return
	}
	s.stats.makeSpaceFor(size)
	s.cache[id] = mmapCache{
		header: genHeader(id, time.Now(), size),
		path:   pastePath,
		mmap:   data,
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
	if err := syscall.Munmap(cached.mmap); err != nil {
		return err
	}
	if err := os.Remove(cached.path); err != nil {
		return err
	}
	s.stats.freeSpace(cached.header.Size)
	return nil
}

func (s *MmapStore) Recover(path string, fileInfo os.FileInfo, err error) error {
	if err != nil || fileInfo.IsDir() {
		return err
	}
	id, err := idFromPath(path)
	if err != nil {
		return err
	}
	modTime := fileInfo.ModTime()
	deathTime := modTime.Add(lifeTime)
	lifeLeft := deathTime.Sub(startTime)
	if lifeTime > 0 && lifeLeft <= 0 {
		return os.Remove(path)
	}
	size := fileInfo.Size()
	s.Lock()
	defer s.Unlock()
	if !s.stats.hasSpaceFor(size) {
		return ErrReachedMax
	}
	pasteFile, err := os.Open(path)
	defer pasteFile.Close()
	mmap, err := getMmap(pasteFile, int(fileInfo.Size()))
	if err != nil {
		return err
	}
	s.stats.makeSpaceFor(size)
	cached := mmapCache{
		header: genHeader(id, modTime, size),
		path:   path,
		mmap:   mmap,
	}
	s.cache[id] = cached
	setupPasteDeletion(s, id, lifeLeft)
	return nil
}

func (s *MmapStore) Report() string {
	s.Lock()
	defer s.Unlock()
	return s.stats.Report()
}

func getMmap(file *os.File, length int) ([]byte, error) {
	fd := int(file.Fd())
	return syscall.Mmap(fd, 0, length, syscall.PROT_READ, syscall.MAP_SHARED)
}
