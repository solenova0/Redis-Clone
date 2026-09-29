package store

import (
	"fmt"
	"strconv"
	"sync"
	"testing"
)

func TestSetGet(t *testing.T) {
	s := New()
	if _, ok, err := s.Get("missing"); ok || err != nil {
		t.Fatalf("Get(missing) = ok %v, err %v", ok, err)
	}
	s.Set("name", []byte("Solan"))
	v, ok, err := s.Get("name")
	if !ok || err != nil || string(v) != "Solan" {
		t.Fatalf("Get(name) = %q, %v, %v", v, ok, err)
	}
	s.Set("name", []byte("other"))
	if v, _, _ := s.Get("name"); string(v) != "other" {
		t.Fatalf("overwrite: got %q", v)
	}
	s.Set("empty", []byte{})
	if v, ok, _ := s.Get("empty"); !ok || len(v) != 0 {
		t.Fatalf("empty value: got %q, %v", v, ok)
	}
}

func TestWrongType(t *testing.T) {
	s := New()
	sh := s.shardFor("k")
	sh.data["k"] = &entry{typ: DataType(99)}
	if _, ok, err := s.Get("k"); !ok || err != ErrWrongType {
		t.Fatalf("got ok %v, err %v; want ErrWrongType", ok, err)
	}
	s.Set("k", []byte("v"))
	if v, _, err := s.Get("k"); err != nil || string(v) != "v" {
		t.Fatalf("SET should replace any type, got %q, %v", v, err)
	}
}

func TestDeleteExists(t *testing.T) {
	s := New()
	s.Set("a", []byte("1"))
	s.Set("b", []byte("2"))

	if n := s.Exists("a", "b", "c", "a"); n != 3 {
		t.Fatalf("Exists = %d, want 3", n)
	}
	if n := s.Delete("a", "c", "a"); n != 1 {
		t.Fatalf("Delete = %d, want 1", n)
	}
	if n := s.Exists("a"); n != 0 {
		t.Fatalf("a still exists")
	}
	if n := s.Len(); n != 1 {
		t.Fatalf("Len = %d, want 1", n)
	}
}

func TestConcurrentAccess(t *testing.T) {
	s := New()
	const workers, ops = 16, 1000
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range ops {
				k := "key:" + strconv.Itoa(i%100)
				switch (w + i) % 4 {
				case 0:
					s.Set(k, []byte(fmt.Sprint(w)))
				case 1:
					s.Get(k)
				case 2:
					s.Exists(k)
				case 3:
					s.Delete(k)
				}
				s.Set(fmt.Sprintf("own:%d:%d", w, i), []byte("x"))
			}
		}()
	}
	wg.Wait()
	for w := range workers {
		for i := range ops {
			if s.Exists(fmt.Sprintf("own:%d:%d", w, i)) != 1 {
				t.Fatalf("lost write own:%d:%d", w, i)
			}
		}
	}
}

func BenchmarkSet(b *testing.B) {
	s := New()
	val := []byte("value")
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			s.Set("key:"+strconv.Itoa(i%10000), val)
			i++
		}
	})
}

func BenchmarkGet(b *testing.B) {
	s := New()
	for i := range 10000 {
		s.Set("key:"+strconv.Itoa(i), []byte("value"))
	}
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			s.Get("key:" + strconv.Itoa(i%10000))
			i++
		}
	})
}
