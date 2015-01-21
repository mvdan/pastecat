package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func strRepeat(s string) string {
	return strings.Repeat(s, idSize/2)
}

func byteRepeat(b byte) []byte {
	return bytes.Repeat([]byte{b}, idSize/2)
}

func TestIDFromString(t *testing.T) {
	for _, c := range []struct {
		in      string
		want    []byte
		wantErr bool
	}{
		{"", nil, true},
		{"invalidhex", nil, true},
		{strings.Repeat("0", idSize-1), nil, true},
		{strings.Repeat("0", idSize+1), nil, true},
		{strRepeat("00"), byteRepeat(0x00), false},
		{strRepeat("01"), byteRepeat(0x01), false},
		{strRepeat("0a"), byteRepeat(0x0a), false},
		{strRepeat("0F"), byteRepeat(0x0f), false},
		{strRepeat("10"), byteRepeat(0x10), false},
		{strRepeat("ee"), byteRepeat(0xee), false},
	} {
		got, err := IDFromString(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf(`IDFromString("%s") didn't error as expected.`, c.in)
			}
		} else if !reflect.DeepEqual(got[:], c.want) {
			t.Errorf(`IDFromString("%s") got %q, want %q`, c.in, got[:], c.want)
		}
	}
}

func TestIDString(t *testing.T) {
	var id ID
	for _, c := range []struct {
		in   []byte
		want string
	}{
		{byteRepeat(0x00), strRepeat("00")},
		{byteRepeat(0xee), strRepeat("ee")},
	} {
		copy(id[:], c.in)
		got := id.String()
		if got != c.want {
			t.Errorf(`ID.String() for %q got "%s", want "%s"`, c.in, got, c.want)
		}
	}
}
