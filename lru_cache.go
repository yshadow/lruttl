// Copyright 2012, Google Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cache borrows heavily from SmallLRUCache (originally by Nathan
// Schrenk). The object maintains a doubly-linked list of elements in the
// When an element is accessed it is promoted to the head of the list, and when
// space is needed the element at the tail of the list (the least recently used
// element) is evicted.
package cache

import (
	"container/list"
	"fmt"
	"sync"
	"time"
)

// Value is value stored in the cache
type Value interface{}

// LRUCache is static and TTL based implementation of LRU cache
type LRUCache struct {
	mu sync.Mutex

	// is this a ttl based evict lru?
	timed bool

	EvictCallback func(string, Value)

	// list & table of *entry objects
	list  *list.List
	table map[string]*list.Element

	// How many items we are limiting the cache to.
	capacity uint64
}

// Item is a container for items listing
type Item struct {
	Key   string
	Value Value
}

type entry struct {
	key          string
	value        Value
	timeAccessed time.Time
}

// NewLRUCache make a new instance of static LRU cache where eviction is based
// on number of items in the cache
func NewLRUCache(capacity uint64) *LRUCache {
	return &LRUCache{
		list:     list.New(),
		table:    make(map[string]*list.Element),
		capacity: capacity,
	}
}

// NewTimeEvictLRU creates a new Lru, which evicts units after a time period of non use
//
//    @ttl is time an entry exists before it is evicted
//    @checkEvery is duration to check every x
//
//    l := NewTimeEvictLru(10000, 200*time.Millisecond, 50*time.Millisecond)
//    var evictCt int = 0
//    l.EvictCallback = func(key string, v Value) {
//    	evictCt++
//    }
func NewTimeEvictLRU(capacity uint64, ttl, checkEvery time.Duration) *LRUCache {

	lru := LRUCache{
		timed:    true,
		list:     list.New(),
		table:    make(map[string]*list.Element),
		capacity: capacity,
	}

	timer := time.NewTicker(checkEvery)
	go func() {
		for {
			select {
			case now := <-timer.C:
				TimeEvict(&lru, now, ttl)
				// TODO case _ = <-done:
				// 	return
			}
		}
	}()

	return &lru
}

// TimeEvict this time eviction is called by a scheduler for background, periodic evictions
func TimeEvict(lru *LRUCache, now time.Time, ttl time.Duration) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	if lru.list.Len() < 1 {
		return
	}

	var n *entry
	var el *list.Element
	hasEvictCallback := lru.EvictCallback != nil

	el = lru.list.Back()

	for {
		if el == nil {
			break
		}
		n = el.Value.(*entry)
		if now.Sub(n.timeAccessed) > ttl {
			// the Difference is greater than max TTL so we need to evict this entry
			// first grab the next entry (as this is about to dissappear)
			el = el.Prev()
			lru.remove(n.key)
			if hasEvictCallback {
				lru.EvictCallback(n.key, n.value)
			}
		} else {
			// since they are stored in time order, as soon as we find
			// first item newer than ttl window, we are safe to bail
			break
		}
	}
}

// Get gets the value identified by a key and ok flag telling if it was found.
// It updates the access time of that item.
func (lru *LRUCache) Get(key string) (v Value, ok bool) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	element := lru.table[key]
	if element == nil {
		return nil, false
	}
	lru.moveToFront(element)
	return element.Value.(*entry).value, true
}

// Peek gets the value identified by a key and ok flag telling if it was found.
// It doesnt' update the access time of that item.
func (lru *LRUCache) Peek(key string) (v Value, ok bool) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	element := lru.table[key]
	if element == nil {
		return nil, false
	}
	return element.Value.(*entry).value, true
}

// Set stores value under a given key
func (lru *LRUCache) Set(key string, value Value) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	if element := lru.table[key]; element != nil {
		lru.updateInplace(element, value)
	} else {
		lru.addNew(key, value)
	}
}

// SetIfAbsent set the value to the cache if it doesn't exist.
// If it exists it just refreshes the access time.
func (lru *LRUCache) SetIfAbsent(key string, value Value) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	if element := lru.table[key]; element != nil {
		lru.moveToFront(element)
	} else {
		lru.addNew(key, value)
	}
}

func (lru *LRUCache) remove(key string) bool {
	element := lru.table[key]
	if element == nil {
		return false
	}

	lru.list.Remove(element)
	delete(lru.table, key)
	return true
}

// Delete removes the value identified by key from the cache and
// returns the flag if it was found.
func (lru *LRUCache) Delete(key string) bool {
	lru.mu.Lock()
	defer lru.mu.Unlock()
	return lru.remove(key)
}

// Clear clears the cache
func (lru *LRUCache) Clear() {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	lru.list.Init()
	lru.table = make(map[string]*list.Element)
}

// SetCapacity sets the capacity of the cache
func (lru *LRUCache) SetCapacity(capacity uint64) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	lru.capacity = capacity
	lru.checkCapacity()
}

// Stats gets the cache statistic (length, capacity, access time of oldest item)
func (lru *LRUCache) Stats() (length, capacity uint64, oldest time.Time) {
	lru.mu.Lock()
	defer lru.mu.Unlock()
	if lastElem := lru.list.Back(); lastElem != nil {
		oldest = lastElem.Value.(*entry).timeAccessed
	}
	return uint64(lru.list.Len()), lru.capacity, oldest
}

// StatsJSON gets the statistic in JSON format
func (lru *LRUCache) StatsJSON() string {
	if lru == nil {
		return "{}"
	}
	l, c, o := lru.Stats()
	return fmt.Sprintf("{\"Length\": %v, \"Capacity\": %v, \"OldestAccess\": \"%v\"}", l, c, o)
}

// Keys gets the keys as a slice of string
func (lru *LRUCache) Keys() []string {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	keys := make([]string, 0, lru.list.Len())
	for e := lru.list.Front(); e != nil; e = e.Next() {
		keys = append(keys, e.Value.(*entry).key)
	}
	return keys
}

// Len gets the current number of items stored in the cache
func (lru *LRUCache) Len() int {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	return lru.list.Len()
}

// Items provides view of Items stored in the cache
func (lru *LRUCache) Items() []Item {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	items := make([]Item, 0, lru.list.Len())
	for e := lru.list.Front(); e != nil; e = e.Next() {
		v := e.Value.(*entry)
		items = append(items, Item{Key: v.key, Value: v.value})
	}
	return items
}

func (lru *LRUCache) updateInplace(element *list.Element, value Value) {
	element.Value.(*entry).value = value
	lru.moveToFront(element)
	lru.checkCapacity()
}

func (lru *LRUCache) moveToFront(element *list.Element) {
	lru.list.MoveToFront(element)
	element.Value.(*entry).timeAccessed = time.Now()
}

func (lru *LRUCache) addNew(key string, value Value) {
	newEntry := &entry{key, value, time.Now()}
	element := lru.list.PushFront(newEntry)
	lru.table[key] = element
	lru.checkCapacity()
}

func (lru *LRUCache) checkCapacity() {
	cl := uint64(lru.list.Len())

	for cl > lru.capacity {
		delElem := lru.list.Back()
		delValue := delElem.Value.(*entry)
		lru.list.Remove(delElem)
		delete(lru.table, delValue.key)
		cl--
	}
}
