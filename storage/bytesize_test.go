/* Copyright (c) 2015, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package storage

import (
	"testing"
)

func TestString(t *testing.T) {
	for _, c := range []struct {
		in   ByteSize
		want string
	}{
		{1, "1.00B"},
		{2 * KB, "2.00KB"},
		{3 * MB, "3.00MB"},
		{4 * GB, "4.00GB"},
		{5 * TB, "5.00TB"},
		{6 * PB, "6.00PB"},
		{7 * EB, "7.00EB"},
		{8 * ZB, "8.00ZB"},
		{9 * YB, "9.00YB"},
	} {
		got := c.in.String()
		if got != c.want {
			t.Errorf("String() got %s, want %s", got, c.want)
		}
	}
}

func TestParse(t *testing.T) {
	for _, c := range []struct {
		in        string
		want      ByteSize
		wantError bool
	}{
		{"", 0, true},
		{" ", 0, true},
		{"0.0.0", 0, true},
		{"-1.0", 0, true},
		{"B", 0, true},
		{"M", 0, true},
		{"0", 0, false},
		{"0.0", 0, false},
		{" 0", 0, false},
		{"0 ", 0, false},
		{"1KB", 1 * KB, false},
		{" 2 KB ", 2 * KB, false},
		{"  3  MB  ", 3 * MB, false},
		{"\t4\tGB\t", 4 * GB, false},
		{"5.0TB", 5 * TB, false},
		{"6.00PB", 6 * PB, false},
		{"7.0000EB", 7 * EB, false},
		{"8ZB", 8 * ZB, false},
		{"9YB", 9 * YB, false},
	} {
		got, err := Parse(c.in)
		if c.wantError && err == nil {
			t.Errorf("Parse(\"%s\") did not error as expected", c.in)
		}
		if got != c.want {
			t.Errorf("Parse(\"%s\") got %s, want %s", c.in, got, c.want)
		}
	}
}

func TestSet(t *testing.T) {
	for _, c := range []struct {
		in        string
		want      ByteSize
		wantError bool
	}{
		{"", 0, true},
		{"0.0.0", 0, true},
		{"0", 0, false},
		{"1KB", 1 * KB, false},
	} {
		var got ByteSize
		err := got.Set(c.in)
		if c.wantError && err == nil {
			t.Errorf("Set(\"%s\") did not error as expected", c.in)
		}
		if got != c.want {
			t.Errorf("Set(\"%s\") got %s, want %s", c.in, got, c.want)
		}
	}
}
