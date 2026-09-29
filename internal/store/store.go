// Package store holds the in-memory keyspace.
//
// Keys are spread across independently locked shards so that clients touching
// different keys rarely contend on the same mutex. Each operation on a single
// key is atomic.
package store

import (
	"errors"
	"hash/maphash"
	"sync"
)

const shardCount = 64

// ErrWrongType is returned when an operation targets a key holding another type.
var ErrWrongType = errors.New("WRONGTYPE Operation against a key holding the wrong kind of value")

// DataType is the Redis type of a stored value.
type DataType uint8

const (
	TypeNone DataType = iota
	TypeString
)

func (t DataType) String() string {
	switch t {
	case TypeString:
		return "string"
	default:
		return "none"
	}
}

// entry is one stored value. Byte slices held in entries are never mutated
// after insertion, so readers may use them after releasing the shard lock.
type entry struct {
	typ DataType
	str []byte
}

type shard struct {
	mu   sync.RWMutex
	data map[string]*entry
}

// Store is a concurrency-safe keyspace.
type Store struct {
	seed   maphash.Seed
	shards [shardCount]shard
}

func New() *Store {
	s := &Store{seed: maphash.MakeSeed()}
	for i := range s.shards {
		s.shards[i].data = make(map[string]*entry)
	}
	return s
}

// A random per-process seed keeps clients from choosing keys that all land in
// one shard.
func (s *Store) shardFor(key string) *shard {
	return &s.shards[maphash.String(s.seed, key)%shardCount]
}

// Get returns the string stored at key. ok is false if the key does not exist.
func (s *Store) Get(key string) (val []byte, ok bool, err error) {
	sh := s.shardFor(key)
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	e, ok := sh.data[key]
	if !ok {
		return nil, false, nil
	}
	if e.typ != TypeString {
		return nil, true, ErrWrongType
	}
	return e.str, true, nil
}

// Set stores val at key, replacing any existing value of any type. The store
// takes ownership of val; the caller must not modify it afterwards.
func (s *Store) Set(key string, val []byte) {
	sh := s.shardFor(key)
	sh.mu.Lock()
	sh.data[key] = &entry{typ: TypeString, str: val}
	sh.mu.Unlock()
}

// Delete removes the given keys and returns how many existed.
func (s *Store) Delete(keys ...string) int {
	n := 0
	for _, k := range keys {
		sh := s.shardFor(k)
		sh.mu.Lock()
		if _, ok := sh.data[k]; ok {
			delete(sh.data, k)
			n++
		}
		sh.mu.Unlock()
	}
	return n
}

// Exists returns how many of keys exist; a key named twice is counted twice.
func (s *Store) Exists(keys ...string) int {
	n := 0
	for _, k := range keys {
		sh := s.shardFor(k)
		sh.mu.RLock()
		if _, ok := sh.data[k]; ok {
			n++
		}
		sh.mu.RUnlock()
	}
	return n
}

// Len returns the number of keys. It is not a point-in-time snapshot across
// shards while writers are active.
func (s *Store) Len() int {
	n := 0
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.RLock()
		n += len(sh.data)
		sh.mu.RUnlock()
	}
	return n
}
