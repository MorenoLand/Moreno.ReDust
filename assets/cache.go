package assets

import (
	"errors"
	"fmt"
	"sync"
)

var ErrResourceCacheClosed = errors.New("APPL resource cache is closed")
var ErrResourceNotCached = errors.New("APPL resource is not cached")
var ErrResourceInUse = errors.New("APPL resources are still referenced")
var ErrResourceReferenceUnderflow = errors.New("APPL resource reference count became negative")
var ErrResourceLeaseClosed = errors.New("APPL resource lease is closed")
var ErrResourceWritebackUnsupported = errors.New("modified APPL resources cannot be written back")

var resourceClock struct {
	mu    sync.Mutex
	ticks uint32
}

func nextResourceTick() uint32 {
	resourceClock.mu.Lock()
	resourceClock.ticks++
	tick := resourceClock.ticks
	resourceClock.mu.Unlock()
	return tick
}

type cachedResource struct {
	index      uint32
	references int16
	flags      uint16
	stamp      uint32
	data       []byte
}

type ResourceCache struct {
	container *Container
	mu        sync.Mutex
	slots     []cachedResource
	capacity  int
	lastIndex uint32
	lastSlot  int
	hasLast   bool
	createdAt uint32
	closed    bool
}

type ResourceLease struct {
	cache    *ResourceCache
	slot     int
	mu       sync.Mutex
	released bool
}

func NewResourceCache(container *Container) (*ResourceCache, error) {
	if container == nil {
		return nil, fmt.Errorf("APPL resource cache requires a container")
	}
	return &ResourceCache{container: container, slots: make([]cachedResource, 0, 20), capacity: 20, createdAt: nextResourceTick()}, nil
}

func (w Workspace) OpenResourceCache(name string) (*ResourceCache, error) {
	container, err := w.OpenContainer(name)
	if err != nil {
		return nil, err
	}
	cache, err := NewResourceCache(container)
	if err != nil {
		container.Close()
	}
	return cache, err
}

func (c *ResourceCache) Header() (Header, error) {
	if c == nil {
		return Header{}, ErrResourceCacheClosed
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return Header{}, ErrResourceCacheClosed
	}
	return c.container.Header(), nil
}

func (c *ResourceCache) Acquire(index uint32) (*ResourceLease, error) {
	if c == nil {
		return nil, ErrResourceCacheClosed
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, ErrResourceCacheClosed
	}
	if slot := c.findSlot(index); slot >= 0 {
		c.slots[slot].references++
		c.slots[slot].stamp = nextResourceTick()
		return &ResourceLease{cache: c, slot: slot}, nil
	}
	data, err := c.container.ReadEntry(index)
	if err != nil {
		return nil, err
	}
	if len(c.slots) == c.capacity {
		c.capacity += 20
		slots := make([]cachedResource, len(c.slots), c.capacity)
		copy(slots, c.slots)
		c.slots = slots
	}
	c.slots = append(c.slots, cachedResource{index: index, references: 1, stamp: nextResourceTick(), data: data})
	slot := len(c.slots) - 1
	c.lastIndex, c.lastSlot, c.hasLast = index, slot, true
	return &ResourceLease{cache: c, slot: slot}, nil
}

func (c *ResourceCache) Release(index uint32) error {
	if c == nil {
		return ErrResourceCacheClosed
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrResourceCacheClosed
	}
	slot := c.findSlot(index)
	if slot < 0 {
		return ErrResourceNotCached
	}
	return c.releaseSlot(slot)
}

func (c *ResourceCache) Evict(index uint32) error {
	if c == nil {
		return ErrResourceCacheClosed
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrResourceCacheClosed
	}
	slot := c.findSlot(index)
	if slot < 0 {
		return ErrResourceNotCached
	}
	if c.slots[slot].references != 0 {
		return ErrResourceInUse
	}
	if c.slots[slot].flags != 0 {
		return ErrResourceWritebackUnsupported
	}
	last := len(c.slots) - 1
	c.slots[slot] = c.slots[last]
	c.slots[last] = cachedResource{}
	c.slots = c.slots[:last]
	c.hasLast = false
	if len(c.slots) < c.capacity-20 {
		c.capacity -= 20
		slots := make([]cachedResource, len(c.slots), c.capacity)
		copy(slots, c.slots)
		c.slots = slots
	}
	return nil
}

func (c *ResourceCache) releaseSlot(slot int) error {
	c.slots[slot].references--
	if c.slots[slot].references < 0 {
		return ErrResourceReferenceUnderflow
	}
	return nil
}

func (c *ResourceCache) findSlot(index uint32) int {
	if c.hasLast && c.lastIndex == index && c.lastSlot < len(c.slots) && c.slots[c.lastSlot].index == index {
		return c.lastSlot
	}
	for slot := range c.slots {
		if c.slots[slot].index == index {
			c.lastIndex, c.lastSlot, c.hasLast = index, slot, true
			return slot
		}
	}
	return -1
}

func (c *ResourceCache) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	for _, slot := range c.slots {
		if slot.references != 0 {
			c.mu.Unlock()
			return ErrResourceInUse
		}
	}
	c.closed = true
	c.slots = nil
	container := c.container
	c.mu.Unlock()
	return container.Close()
}

func (l *ResourceLease) Bytes() ([]byte, error) {
	if l == nil || l.cache == nil {
		return nil, ErrResourceLeaseClosed
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.released {
		return nil, ErrResourceLeaseClosed
	}
	c := l.cache
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, ErrResourceCacheClosed
	}
	if l.slot < 0 || l.slot >= len(c.slots) {
		return nil, ErrResourceNotCached
	}
	return append([]byte(nil), c.slots[l.slot].data...), nil
}

func (l *ResourceLease) Close() error {
	if l == nil || l.cache == nil {
		return ErrResourceLeaseClosed
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.released {
		return ErrResourceLeaseClosed
	}
	l.released = true
	c := l.cache
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrResourceCacheClosed
	}
	if l.slot < 0 || l.slot >= len(c.slots) {
		return ErrResourceNotCached
	}
	return c.releaseSlot(l.slot)
}
