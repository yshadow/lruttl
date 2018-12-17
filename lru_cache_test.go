// Copyright 2012, Google Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type CacheValue struct {
	val int
}

func TestInitialState(t *testing.T) {
	cache := NewLRUCache(5)
	l, c, _ := cache.Stats()
	if l != 0 {
		t.Errorf("length = %v, want 0", l)
	}
	if c != 5 {
		t.Errorf("capacity = %v, want 5", c)
	}
}

func TestSetInsertsValue(t *testing.T) {
	cache := NewLRUCache(100)
	data := &CacheValue{0}
	key := "key"
	cache.Set(key, data)

	v, ok := cache.Get(key)
	if !ok || v.(*CacheValue) != data {
		t.Errorf("Cache has incorrect value: %v != %v", data, v)
	}
}

func TestGetValueWithMultipleTypes(t *testing.T) {
	cache := NewLRUCache(100)
	data := &CacheValue{0}
	key := "key"
	cache.Set(key, data)

	v, ok := cache.Get("key")
	if !ok || v.(*CacheValue) != data {
		t.Errorf("Cache has incorrect value for \"key\": %v != %v", data, v)
	}

	v, ok = cache.Get(string([]byte{'k', 'e', 'y'}))
	if !ok || v.(*CacheValue) != data {
		t.Errorf("Cache has incorrect value for []byte {'k','e','y'}: %v != %v", data, v)
	}
}

func TestSetUpdatesSize(t *testing.T) {
	cache := NewLRUCache(100)
	emptyValue := &CacheValue{0}
	key := "key1"
	cache.Set(key, emptyValue)
	if ln, _, _ := cache.Stats(); ln != 1 {
		t.Errorf("cache.Size() = %v, expected 1", ln)
	}
	someValue := &CacheValue{20}
	key = "key2"
	cache.Set(key, someValue)
	if ln, _, _ := cache.Stats(); ln != 2 {
		t.Errorf("cache.Size() = %v, expected 2", ln)
	}
}

func TestSetWithOldKeyUpdatesValue(t *testing.T) {
	cache := NewLRUCache(100)
	emptyValue := &CacheValue{0}
	key := "key1"
	cache.Set(key, emptyValue)
	someValue := &CacheValue{20}
	cache.Set(key, someValue)

	v, ok := cache.Get(key)
	if !ok || v.(*CacheValue) != someValue {
		t.Errorf("Cache has incorrect value: %v != %v", someValue, v)
	}
}

func TestSetWithOldKeyUpdatesSize(t *testing.T) {
	cache := NewLRUCache(100)
	emptyValue := &CacheValue{0}
	key := "key1"
	cache.Set(key, emptyValue)

	if ln, _, _ := cache.Stats(); ln != 1 {
		t.Errorf("cache.Size() = %v, expected %v", ln, 1)
	}

	someValue := &CacheValue{20}
	cache.Set(key, someValue)
	expected := uint64(1)
	if ln, _, _ := cache.Stats(); ln != expected {
		t.Errorf("cache.Size() = %v, expected %v", ln, expected)
	}
}

func TestGetNonExistent(t *testing.T) {
	cache := NewLRUCache(100)

	if _, ok := cache.Get("crap"); ok {
		t.Error("Cache returned a crap value after no inserts.")
	}
}

func TestDelete(t *testing.T) {
	cache := NewLRUCache(100)
	value := &CacheValue{1}
	key := "key"

	if cache.Delete(key) {
		t.Error("Item unexpectedly already in cache.")
	}

	cache.Set(key, value)

	if !cache.Delete(key) {
		t.Error("Expected item to be in cache.")
	}

	if ln, _, _ := cache.Stats(); ln != 0 {
		t.Errorf("cache.Size() = %v, expected 0", ln)
	}

	if _, ok := cache.Get(key); ok {
		t.Error("Cache returned a value after deletion.")
	}
}

func TestClear(t *testing.T) {
	cache := NewLRUCache(100)
	value := &CacheValue{1}
	key := "key"

	cache.Set(key, value)
	cache.Clear()

	if ln, _, _ := cache.Stats(); ln != 0 {
		t.Errorf("cache.Size() = %v, expected 0 after Clear()", ln)
	}
}

func TestCapacityIsObeyed(t *testing.T) {
	size := uint64(3)
	cache := NewLRUCache(size)
	value := &CacheValue{1}

	// Insert up to the cache's capacity.
	cache.Set("key1", value)
	cache.Set("key2", value)
	cache.Set("key3", value)
	if ln, _, _ := cache.Stats(); ln != size {
		t.Errorf("cache.Size() = %v, expected %v", ln, size)
	}
	// Insert one more; something should be evicted to make room.
	cache.Set("key4", value)
	if ln, _, _ := cache.Stats(); ln != size {
		t.Errorf("post-evict cache.Size() = %v, expected %v", ln, size)
	}
}

func TestLRUIsEvicted(t *testing.T) {
	size := uint64(3)
	cache := NewLRUCache(size)

	cache.Set("key1", &CacheValue{1})
	cache.Set("key2", &CacheValue{1})
	cache.Set("key3", &CacheValue{1})
	// lru: [key3, key2, key1]

	// Look up the elements. This will rearrange the LRU ordering.
	cache.Get("key3")
	cache.Get("key2")
	cache.Get("key1")
	// lru: [key1, key2, key3]

	cache.Set("key0", &CacheValue{1})
	// lru: [key0, key1, key2]

	// The least recently used one should have been evicted.
	if _, ok := cache.Get("key3"); ok {
		t.Error("Least recently used element was not evicted.")
	}
}

func TestLRUTTLEvict(t *testing.T) {

	// ttl = 1 second, 100 ms heartbeats, should check 10 times
	l := NewTimeEvictLRU(100, 100*time.Millisecond, 100*time.Millisecond)

	var evictCt int
	expectedEvictCt := 100

	l.EvictCallback = func(key string, v Value) {
		evictCt++
		//u.Debug("evicting ", key)
	}

	for i := 0; i < 100; i++ {
		istr := strconv.FormatInt(int64(i), 10)
		l.Set("key"+istr, &CacheValue{i})
	}

	time.Sleep(time.Second)

	assert.Equal(t, expectedEvictCt, evictCt, "Evict function should be called %d times", expectedEvictCt)
	assert.Equal(t, 0, l.Len(), "Cache should be empty at the end")
}
