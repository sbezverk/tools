package store

import (
	"errors"
)

var (
	// ErrAlreadyExist error returns when Add attempts to add already existing item
	ErrAlreadyExist = errors.New("already exists")
	// ErrNotFound error returns when Get attempts to get a non existing item
	ErrNotFound = errors.New("not found")
)

type storeOp uint8

const (
	addItem storeOp = iota + 1
	removeItem
	getItem
	listItems
)

type Storable interface {
	Key() string
}

var _ Storable = &item{}

type item struct {
	key string
}

func (i *item) Key() string {
	return i.key
}

type Manager interface {
	Add(Storable) error
	Remove(string) error
	List() []Storable
	Get(string) Storable
	Stop()
}

var _ Manager = &itemStore{}

type mgrReply struct {
	item []Storable
	err  error
}

type storeCh struct {
	op      storeOp
	item    []Storable
	replyCh chan *mgrReply
	err     chan error
}

type itemStore struct {
	stopCh chan struct{}
	opCh   chan *storeCh
}

func (s *itemStore) Add(i Storable) error {
	err := make(chan error)
	s.opCh <- &storeCh{
		op:   addItem,
		item: []Storable{i},
		err:  err,
	}
	// Return the result of the operation
	return <-err
}

func (s *itemStore) Remove(key string) error {
	err := make(chan error)
	s.opCh <- &storeCh{
		op:   removeItem,
		item: []Storable{&item{key: key}},
		err:  err,
	}
	// Just wait for the operation to complete, no result is needed
	return <-err
}

func (s *itemStore) Get(key string) Storable {
	repl := make(chan *mgrReply)
	s.opCh <- &storeCh{
		op: getItem,
		item: []Storable{
			&item{
				key: key,
			},
		},
		replyCh: repl,
	}
	r := <-repl
	if len(r.item) == 0 {
		return nil
	}

	return r.item[0]
}

func (s *itemStore) List() []Storable {
	repl := make(chan *mgrReply)
	s.opCh <- &storeCh{
		op:      listItems,
		replyCh: repl,
	}
	r := <-repl

	return r.item
}

func (s *itemStore) Stop() {
	close(s.stopCh)
}

func (s *itemStore) manager() {
	items := make(map[string]Storable)
	for {
		select {
		case <-s.stopCh:
			return
		case msg := <-s.opCh:
			switch msg.op {
			case addItem:
				if _, ok := items[msg.item[0].Key()]; ok {
					msg.err <- ErrAlreadyExist
					continue
				}
				items[msg.item[0].Key()] = msg.item[0]
				msg.err <- nil
			case removeItem:
				if _, ok := items[msg.item[0].Key()]; !ok {
					msg.err <- ErrNotFound
					continue
				}
				delete(items, msg.item[0].Key())
				msg.err <- nil
			case getItem:
				it, ok := items[msg.item[0].Key()]
				if !ok {
					msg.replyCh <- &mgrReply{
						item: nil,
						err:  ErrNotFound,
					}
					continue
				}
				msg.replyCh <- &mgrReply{
					item: []Storable{it},
					err:  nil,
				}
			case listItems:
				l := make([]Storable, 0)
				for _, item := range items {
					l = append(l, item)
				}
				msg.replyCh <- &mgrReply{
					item: l,
					err:  nil,
				}
			}
		}
	}
}

// NewStore returns a new instance of a store, any object which is compatible
// with the interface Storable, can be stored in the store.
func NewStore() Manager {
	s := &itemStore{
		stopCh: make(chan struct{}),
		opCh:   make(chan *storeCh),
	}
	// Starting store manager
	go s.manager()

	return s
}
