/* Copyright (c) 2014, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type FileStore struct {
	sync.RWMutex
	cache map[ID]fileCache

	dir      string
	lifeTime time.Duration
	stats    Stats
}

type fileCache struct {
	header Header
	path   string
}

func newFileStore(dir string, maxNumber int, maxStorage ByteSize, lifeTime time.Duration) (s *FileStore, err error) {

	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err = os.Chdir(dir); err != nil {
		return nil, err
	}

	s = new(FileStore)
	s.dir = dir
	s.cache = make(map[ID]fileCache)
	s.lifeTime = lifeTime
	s.stats = Stats{maxNumber: maxNumber, maxStorage: maxStorage}

	for i := 0; i < 256; i++ {
		if err = s.setupSubdir(byte(i)); err != nil {
			return nil, err
		}
	}
	return
}

func (s *FileStore) Get(id ID) (Content, *Header, error) {
	s.RLock()
	defer s.RUnlock()
	cached, e := s.cache[id]
	if !e {
		return nil, nil, ErrPasteNotFound
	}
	f, err := os.Open(cached.path)
	if err != nil {
		return nil, nil, err
	}
	return f, &cached.header, nil
}

func (s *FileStore) Put(content []byte) (id ID, err error) {
	s.Lock()
	defer s.Unlock()
	size := ByteSize(len(content))
	if !s.stats.hasSpaceFor(size) {
		return id, ErrReachedMax
	}
	if id, err = s.randomID(); err != nil {
		return
	}
	hexID := id.String()
	pastePath := path.Join(hexID[:2], hexID[2:])
	pasteFile, err := os.OpenFile(pastePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return
	}
	defer pasteFile.Close()
	if _, err = pasteFile.Write(content); err != nil {
		return
	}
	s.stats.makeSpaceFor(size)
	s.cache[id] = fileCache{
		header: genHeader(id, s.lifeTime, time.Now(), size),
		path:   pastePath,
	}
	return id, nil
}

func (s *FileStore) Delete(id ID) error {
	s.Lock()
	defer s.Unlock()
	cached, e := s.cache[id]
	if !e {
		return ErrPasteNotFound
	}
	delete(s.cache, id)
	if err := os.Remove(cached.path); err != nil {
		return err
	}
	s.stats.freeSpace(cached.header.Size)
	return nil
}

func (s *FileStore) Recover(pastePath string, fileInfo os.FileInfo, err error) error {
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
	deathTime := modTime.Add(s.lifeTime)
	if s.lifeTime > 0 {
		if deathTime.Before(startTime) {
			return os.Remove(pastePath)
		}
	}
	size := ByteSize(fileInfo.Size())
	s.Lock()
	defer s.Unlock()
	if !s.stats.hasSpaceFor(size) {
		return ErrReachedMax
	}
	s.stats.makeSpaceFor(size)
	lifeLeft := deathTime.Sub(startTime)
	cached := fileCache{
		header: genHeader(id, s.lifeTime, modTime, size),
		path:   pastePath,
	}
	s.cache[id] = cached
	SetupPasteDeletion(s, id, lifeLeft)
	return nil
}

func (s *FileStore) randomID() (id ID, err error) {
	for try := 0; try < randTries; try++ {
		if _, err := rand.Read(id[:]); err != nil {
			continue
		}
		if _, e := s.cache[id]; !e {
			return id, nil
		}
	}
	return id, ErrNoUnusedIDFound
}

func (s *FileStore) setupSubdir(h byte) error {
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

func (s *FileStore) Report() string {
	s.Lock()
	defer s.Unlock()
	return s.stats.Report()
}
