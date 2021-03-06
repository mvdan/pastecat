package storage

import (
	"testing"
)

func TestMakeSpaceFor(t *testing.T) {
	for _, c := range [...]struct {
		inNumber     int
		inMaxNumber  int
		inStorage    int64
		inMaxStorage int64
		inSize       int64
		want         error
	}{
		{0, 0, 0, 0, 1, nil},
		{0, 5, 0, 5, 3, nil},
		{0, 1, 0, 1, 1, nil},
		{1, 1, 0, 1, 1, ErrReachedMaxNumber},
		{5, 1, 0, 1, 1, ErrReachedMaxNumber},
		{0, 1, 1, 1, 1, ErrReachedMaxStorage},
		{0, 1, 5, 1, 3, ErrReachedMaxStorage},
	} {
		stats := Stats{
			number:     c.inNumber,
			MaxNumber:  c.inMaxNumber,
			storage:    c.inStorage,
			MaxStorage: c.inMaxStorage,
		}
		got := stats.MakeSpaceFor(c.inSize)
		if got != c.want {
			t.Errorf(`%+v.MakeSpaceFor(%v) didn't error as expected.`, stats, c.inSize)
		}
	}
}

func TestSpaceChanges(t *testing.T) {
	stats := Stats{MaxNumber: 2, MaxStorage: 20}
	mustSucceed := func(err error) {
		if err != nil {
			t.Errorf("Encountered unexpected error")
		}
	}
	mustError := func(err error) {
		if err == nil {
			t.Errorf("Did not error as expected")
		}
	}
	mustSucceed(stats.MakeSpaceFor(1))
	mustSucceed(stats.MakeSpaceFor(1))
	mustError(stats.MakeSpaceFor(1))
	stats.FreeSpace(1)
	mustSucceed(stats.MakeSpaceFor(1))
	stats.FreeSpace(1)
	stats.FreeSpace(1)
	mustSucceed(stats.MakeSpaceFor(15))
	mustError(stats.MakeSpaceFor(15))
}
