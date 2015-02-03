package main

import (
	"testing"
)


func TestRandomID(t *testing.T) {
	for _, c := range []struct {
		available func(ID) bool
		wantErr   bool
	}{
		{func(id ID) bool { return true; }, false},
		{func(id ID) bool { return false; }, true},
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
