/* Copyright (c) 2014-2015, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
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
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err = os.Chdir(dir); err != nil {
		return nil, err
	}
	s = new(MmapStore)
	s.dir = dir
	s.cache = make(map[ID]mmapCache)
	for i := 0; i < 256; i++ {
		if err = s.setupSubdir(byte(i)); err != nil {
			return nil, err
		}
	}
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
	hexID := id.String()
	pastePath := path.Join(hexID[:2], hexID[2:])
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

func (s *MmapStore) Recover(pastePath string, fileInfo os.FileInfo, err error) error {
	if err != nil {
		return err
	}
	if fileInfo.IsDir() {
		return nil
	}
	parts := strings.Split(pastePath, string(filepath.Separator))
	if len(parts) != 2 {
		return errors.New("invalid number of directories at " + pastePath)
	}
	hexID := parts[0] + parts[1]
	id, err := IDFromString(hexID)
	if err != nil {
		return err
	}
	modTime := fileInfo.ModTime()
	size := fileInfo.Size()
	s.Lock()
	defer s.Unlock()
	if !s.stats.hasSpaceFor(size) {
		return ErrReachedMax
	}
	pasteFile, err := os.Open(pastePath)
	defer pasteFile.Close()
	mmap, err := getMmap(pasteFile, int(fileInfo.Size()))
	if err != nil {
		return err
	}
	s.stats.makeSpaceFor(size)
	cached := mmapCache{
		header: genHeader(id, modTime, size),
		path:   pastePath,
		mmap:   mmap,
	}
	if lifeTime > 0 {
		deathTime := modTime.Add(lifeTime)
		if deathTime.Before(startTime) {
			return os.Remove(pastePath)
		}
	}
	s.cache[id] = cached
	return nil
}

func (s *MmapStore) Report() string {
	s.Lock()
	defer s.Unlock()
	return s.stats.Report()
}

func (s *MmapStore) setupSubdir(h byte) error {
	dir := hex.EncodeToString([]byte{h})
	if stat, err := os.Stat(dir); err == nil {
		if !stat.IsDir() {
			return fmt.Errorf("%s/%s exists but is not a directory", s.dir, dir)
		}
		if err := filepath.Walk(dir, s.Recover); err != nil {
			return fmt.Errorf("cannot recover data directory %s/%s: %s", s.dir, dir, err)
		}
	} else if err := os.Mkdir(dir, 0700); err != nil {
		return fmt.Errorf("cannot create data directory %s/%s: %s", s.dir, dir, err)
	}
	return nil
}

func getMmap(file *os.File, length int) ([]byte, error) {
	fd := int(file.Fd())
	prot := syscall.PROT_READ
	flags := syscall.MAP_SHARED
	return syscall.Mmap(fd, 0, length, prot, flags)
}
