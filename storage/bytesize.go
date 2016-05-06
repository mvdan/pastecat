/* Copyright (c) 2015, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package storage

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

var regexByteSize = regexp.MustCompile(`^\s*([\d\.]+)\s*([KMGTPEZY]?B?)\s*$`)

type ByteSize float64

const (
	_           = iota
	KB ByteSize = 1 << (10 * iota)
	MB
	GB
	TB
	PB
	EB
	ZB
	YB
)

func (b ByteSize) String() string {
	switch {
	case b >= YB:
		return fmt.Sprintf("%.2fYB", b/YB)
	case b >= ZB:
		return fmt.Sprintf("%.2fZB", b/ZB)
	case b >= EB:
		return fmt.Sprintf("%.2fEB", b/EB)
	case b >= PB:
		return fmt.Sprintf("%.2fPB", b/PB)
	case b >= TB:
		return fmt.Sprintf("%.2fTB", b/TB)
	case b >= GB:
		return fmt.Sprintf("%.2fGB", b/GB)
	case b >= MB:
		return fmt.Sprintf("%.2fMB", b/MB)
	case b >= KB:
		return fmt.Sprintf("%.2fKB", b/KB)
	}
	return fmt.Sprintf("%.2fB", b)
}

func parseBytesize(s string) (ByteSize, error) {
	parts := regexByteSize.FindStringSubmatch(s)
	if parts == nil {
		return 0, errors.New("invalid byte size")
	}
	f, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return 0, err
	}
	size := ByteSize(f)
	switch parts[2] {
	case "KB", "K":
		size *= KB
	case "MB", "M":
		size *= MB
	case "GB", "G":
		size *= GB
	case "TB", "T":
		size *= TB
	case "PB", "P":
		size *= PB
	case "EB", "E":
		size *= EB
	case "ZB", "Z":
		size *= ZB
	case "YB", "Y":
		size *= YB
	}
	return size, nil
}

func (b *ByteSize) Set(value string) error {
	size, err := parseBytesize(value)
	if err != nil {
		return err
	}
	*b = size
	return nil
}
