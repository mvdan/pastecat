package main

import (
	"testing"
)

func TestRandomID(t *testing.T) {
	countFalse := func(count int) func(ID) bool {
		cur := 0
		return func(ID) bool {
			if cur >= count {
				return true
			}
			cur++
			return false
		}
	}
	for _, c := range []struct {
		available func(ID) bool
		wantErr   bool
	}{
		{func(id ID) bool { return true }, false},
		{func(id ID) bool { return false }, true},
		{countFalse(randTries - 1), false},
		{countFalse(randTries + 1), true},
	} {
		_, err := randomID(c.available)
		if c.wantErr {
			if err == nil {
				t.Errorf(`randomID() didn't error as expected`)
			}
		} else if err != nil {
			t.Errorf(`randomID() errored unexpectedly`)
		}
	}
}
