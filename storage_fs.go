/* Copyright (c) 2014-2015, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"encoding/hex"
	"fmt"
	"io"
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

	dir   string
	stats Stats
}

type fileCache struct {
	reading sync.WaitGroup
	header  Header
	path    string
}

type fileContent struct {
	file    *os.File
	reading *sync.WaitGroup
}

func (c fileContent) Read(p []byte) (n int, err error) {
	return c.file.Read(p)
}

func (c fileContent) ReadAt(p []byte, off int64) (n int, err error) {
	return c.file.ReadAt(p, off)
}

func (c fileContent) Seek(offset int64, whence int) (int64, error) {
	return c.file.Seek(offset, whence)
}

func (c fileContent) Close() error {
	err := c.file.Close()
	c.reading.Done()
	return err
}

func newFileStore(dir string) (s *FileStore, err error) {
	setupTopDir(dir)
	s = new(FileStore)
	s.dir = dir
	s.cache = make(map[ID]fileCache)
	setupSubdirs(s.dir, s.Recover)
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
	cached.reading.Add(1)
	return fileContent{f, &cached.reading}, &cached.header, nil
}

func writeNewFile(filename string, data []byte) error {
	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	n, err := f.Write(data)
	if err == nil && n < len(data) {
		err = io.ErrShortWrite
	}
	if err1 := f.Close(); err == nil {
		err = err1
	}
	return err
}

func (s *FileStore) Put(content []byte) (id ID, err error) {
	s.Lock()
	defer s.Unlock()
	size := int64(len(content))
	if err = s.stats.hasSpaceFor(size); err != nil {
		return id, err
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
	s.stats.makeSpaceFor(size)
	s.cache[id] = fileCache{
		header: genHeader(id, time.Now(), size),
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
	cached.reading.Wait()
	if err := os.Remove(cached.path); err != nil {
		return err
	}
	s.stats.freeSpace(cached.header.Size)
	return nil
}

func pathFromID(id ID) string {
	hexID := id.String()
	return path.Join(hexID[:2], hexID[2:])
}

func idFromPath(path string) (id ID, err error) {
	parts := strings.Split(path, string(filepath.Separator))
	if len(parts) != 2 {
		return id, fmt.Errorf("invalid number of directories at %s", path)
	}
	hexID := parts[0] + parts[1]
	return IDFromString(hexID)
}

func (s *FileStore) Recover(path string, fileInfo os.FileInfo, err error) error {
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
	if size == 0 {
		return os.Remove(path)
	}
	s.Lock()
	defer s.Unlock()
	if err := s.stats.hasSpaceFor(size); err != nil {
		return err
	}
	s.stats.makeSpaceFor(size)
	cached := fileCache{
		header: genHeader(id, modTime, size),
		path:   path,
	}
	s.cache[id] = cached
	setupPasteDeletion(s, id, lifeLeft)
	return nil
}

func setupTopDir(topdir string) error {
	if err := os.MkdirAll(topdir, 0700); err != nil {
		return err
	}
	return os.Chdir(topdir)
}

func setupSubdirs(topdir string, rec func(string, os.FileInfo, error) error) error {
	for i := 0; i < 256; i++ {
		if err := setupSubdir(topdir, rec, byte(i)); err != nil {
			return err
		}
	}
	return nil
}

func setupSubdir(topdir string, rec func(string, os.FileInfo, error) error, h byte) error {
	dir := hex.EncodeToString([]byte{h})
	if stat, err := os.Stat(dir); err == nil {
		if !stat.IsDir() {
			return fmt.Errorf("%s/%s exists but is not a directory", topdir, dir)
		}
		if err := filepath.Walk(dir, rec); err != nil {
			return fmt.Errorf("cannot recover data directory %s/%s: %s", topdir, dir, err)
		}
	} else if err := os.Mkdir(dir, 0700); err != nil {
		return fmt.Errorf("cannot create data directory %s/%s: %s", topdir, dir, err)
	}
	return nil
}

func (s *FileStore) Report() string {
	s.Lock()
	defer s.Unlock()
	return s.stats.Report()
}
