package assets

import (
	"errors"
	"fmt"
	"sync"
)

var ErrNativeMemoryCallbacksMissing = errors.New("native memory callbacks are unavailable")
var ErrNativeMemoryReserve = errors.New("native resource memory reserve was not met")
var ErrNativeHandleMissing = errors.New("native resource handle is not cached")
var ErrNativeHandleReferenceUnderflow = errors.New("native resource handle reference count became negative")

type NativeResourceError struct {
	Code  uint16
	Cause error
}

func (e *NativeResourceError) Error() string {
	if e.Cause == nil {
		return fmt.Sprintf("native resource manager error 0x%04x", e.Code)
	}
	return fmt.Sprintf("native resource manager error 0x%04x: %v", e.Code, e.Cause)
}

func (e *NativeResourceError) Unwrap() error {
	return e.Cause
}

type NativeMemoryStatus func() (pageFile, virtual, totalPhysical uint32)
type NativeCacheEvictor func(required *int32) (uint32, error)

type NativeMemoryHooks struct {
	Status     NativeMemoryStatus
	EvictOther NativeCacheEvictor
}

type handlePayload struct {
	data []byte
	size int
}

type handleEntry struct {
	manager    *ResourceCache
	index      uint32
	stamp      int32
	handle     *handlePayload
	references int16
	flags      uint16
}

type HandleCache struct {
	mu       sync.Mutex
	entries  []handleEntry
	capacity int
	memory   NativeMemoryHooks
}

type HandleLease struct {
	cache  *HandleCache
	handle *handlePayload
}

func NewHandleCache(memory NativeMemoryHooks) (*HandleCache, error) {
	if memory.Status == nil || memory.EvictOther == nil {
		return nil, ErrNativeMemoryCallbacksMissing
	}
	return &HandleCache{entries: make([]handleEntry, 0, 10), capacity: 10, memory: memory}, nil
}

func NativeAvailableMemory(pageFile, virtual, totalPhysical uint32) uint32 {
	if pageFile < virtual {
		virtual = pageFile
	}
	if totalPhysical < virtual {
		virtual = totalPhysical
	}
	return virtual
}

func (c *HandleCache) Acquire(manager *ResourceCache, index uint32) (*HandleLease, error) {
	if c == nil || manager == nil {
		return nil, &NativeResourceError{Code: 0x145e, Cause: ErrNativeHandleMissing}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if slot := c.findResource(manager, index); slot >= 0 {
		entry := &c.entries[slot]
		entry.references++
		entry.stamp = int32(nextResourceTick())
		return &HandleLease{cache: c, handle: entry.handle}, nil
	}
	resource, err := manager.Acquire(index)
	if err != nil {
		return nil, &NativeResourceError{Code: 0x145e, Cause: err}
	}
	size, err := resource.Size()
	if err != nil {
		if releaseErr := resource.Close(); releaseErr != nil {
			return nil, &NativeResourceError{Code: 0x1461, Cause: releaseErr}
		}
		return nil, &NativeResourceError{Code: 0x145e, Cause: err}
	}
	if err := c.ensureMemory(int32(size) + 20000); err != nil {
		if releaseErr := resource.Close(); releaseErr != nil {
			return nil, &NativeResourceError{Code: 0x1461, Cause: releaseErr}
		}
		return nil, err
	}
	data, err := resource.Bytes()
	if err != nil {
		if releaseErr := resource.Close(); releaseErr != nil {
			return nil, &NativeResourceError{Code: 0x1461, Cause: releaseErr}
		}
		return nil, &NativeResourceError{Code: 0x145e, Cause: err}
	}
	if err := resource.Close(); err != nil {
		return nil, &NativeResourceError{Code: 0x1461, Cause: err}
	}
	payload := &handlePayload{data: data, size: int(size)}
	if len(payload.data) == 0 {
		payload.data = make([]byte, 1)
	}
	if len(c.entries) == c.capacity {
		c.capacity += 10
		entries := make([]handleEntry, len(c.entries), c.capacity)
		copy(entries, c.entries)
		c.entries = entries
	}
	entry := handleEntry{manager: manager, index: index, stamp: int32(nextResourceTick()), handle: payload, references: 1}
	c.entries = append(c.entries, entry)
	return &HandleLease{cache: c, handle: payload}, nil
}

func (c *HandleCache) ensureMemory(required int32) error {
	pageFile, virtual, totalPhysical := c.memory.Status()
	available := NativeAvailableMemory(pageFile, virtual, totalPhysical)
	if int32(uint32(required)-available+300000) < 0 {
		return nil
	}
	remaining := int32(uint32(required) - available + 500000)
	status, err := c.memory.EvictOther(&remaining)
	if err != nil {
		return &NativeResourceError{Code: 0x145f, Cause: err}
	}
	if uint16(status) != 0 {
		return &NativeResourceError{Code: 0x145f, Cause: fmt.Errorf("first-level eviction returned 0x%04x", uint16(status))}
	}
	status = c.evictForMemory(&remaining)
	if uint16(status) != 0 {
		return &NativeResourceError{Code: 0x145f, Cause: fmt.Errorf("handle-cache eviction returned 0x%04x", uint16(status))}
	}
	if remaining >= 0 {
		return &NativeResourceError{Code: 0x1460, Cause: ErrNativeMemoryReserve}
	}
	return nil
}

func (c *HandleCache) findResource(manager *ResourceCache, index uint32) int {
	for slot := range c.entries {
		if c.entries[slot].manager == manager && c.entries[slot].index == index {
			return slot
		}
	}
	return -1
}

func (c *HandleCache) findHandle(handle *handlePayload) int {
	for slot := range c.entries {
		if c.entries[slot].handle == handle {
			return slot
		}
	}
	return -1
}

func (c *HandleCache) evictForMemory(required *int32) uint32 {
	for {
		slot := -1
		oldest := int32(0x7fffffff)
		for i := range c.entries {
			entry := &c.entries[i]
			if entry.references == 0 && entry.stamp < oldest {
				slot = i
				oldest = entry.stamp
			}
		}
		if slot < 0 {
			return 0
		}
		*required -= int32(uint32(len(c.entries[slot].handle.data)))
		c.removeEntry(slot)
		if *required < 1 {
			return 0
		}
	}
}

func (c *HandleCache) removeEntry(slot int) {
	entry := c.entries[slot]
	if entry.handle != nil {
		entry.handle.data = nil
		entry.handle.size = 0
	}
	last := len(c.entries) - 1
	c.entries[slot] = c.entries[last]
	c.entries[last] = handleEntry{}
	c.entries = c.entries[:last]
}

func (l *HandleLease) Bytes() ([]byte, error) {
	if l == nil || l.cache == nil || l.handle == nil {
		return nil, &NativeResourceError{Code: 0x1463, Cause: ErrNativeHandleMissing}
	}
	c := l.cache
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.findHandle(l.handle) < 0 {
		return nil, &NativeResourceError{Code: 0x1463, Cause: ErrNativeHandleMissing}
	}
	return l.handle.data[:l.handle.size], nil
}

func (l *HandleLease) Close() error {
	if l == nil || l.cache == nil || l.handle == nil {
		return &NativeResourceError{Code: 0x1463, Cause: ErrNativeHandleMissing}
	}
	c := l.cache
	c.mu.Lock()
	defer c.mu.Unlock()
	slot := c.findHandle(l.handle)
	if slot < 0 {
		return &NativeResourceError{Code: 0x1463, Cause: ErrNativeHandleMissing}
	}
	c.entries[slot].references--
	if c.entries[slot].references < 0 {
		return &NativeResourceError{Code: 0x1462, Cause: ErrNativeHandleReferenceUnderflow}
	}
	return nil
}
