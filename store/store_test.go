package store

import (
	"reflect"
	"testing"
)

type testItem struct {
	key string
}

func (i *testItem) Key() string {
	return i.key
}

var (
	i1 = &testItem{
		key: "1",
	}
	i2 = &testItem{
		key: "2",
	}
	i3 = &testItem{
		key: "3",
	}
)

func TestAdd(t *testing.T) {
	s := NewStore()
	if err := s.Add(i1); err != nil {
		t.Fatalf("supposed to succeed but failed with error: %+v", err)
	}
	if err := s.Add(i1); err == nil {
		t.Fatalf("supposed to fail but succeeded")
	}
}

func TestRemove(t *testing.T) {
	s := NewStore()
	s.Add(i2)
	s.Add(i3)

	if err := s.Remove(i2); err != nil {
		t.Fatalf("supposed to succeed but failed with error: %+v", err)
	}
	if err := s.Remove(i1); err == nil {
		t.Fatalf("supposed to fail but succeeded")
	}
}

func TestGet(t *testing.T) {
	s := NewStore()
	s.Add(i1)
	s.Add(i3)

	i := s.Get(i1.Key())
	if i == nil {
		t.Fatalf("item %s supposed to be found", i1.key)
	}
	if !reflect.DeepEqual(i1, i.(*testItem)) {
		t.Fatalf("original item %s and recovered %s do not match", i1.key, i.(*testItem).Key())
	}
	if i := s.Get(i2.Key()); i != nil {
		t.Fatalf("item %s is not supposed to be found", i2.key)
	}
}

func TesList(t *testing.T) {
	s := NewStore()
	s.Add(i1)
	s.Add(i2)
	s.Add(i3)

	if len(s.List()) != 3 {
		t.Fatalf("store supposed to have 3 items")
	}
}
