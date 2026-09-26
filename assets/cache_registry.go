package assets

import (
	"errors"
	"sync"
)

var ErrResourceCacheRegistryUnavailable = errors.New("resource cache registry is unavailable")
var ErrResourceCacheAlreadyRegistered = errors.New("resource cache is already registered")

type ResourceCacheRegistry struct {
	mu       sync.Mutex
	managers []*ResourceCache
}

var processResourceCacheRegistry = NewResourceCacheRegistry()

func NewResourceCacheRegistry() *ResourceCacheRegistry {
	return &ResourceCacheRegistry{}
}

func ProcessResourceCacheRegistry() *ResourceCacheRegistry {
	return processResourceCacheRegistry
}

func (r *ResourceCacheRegistry) Head() *ResourceCache {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.managers) == 0 {
		return nil
	}
	return r.managers[0]
}

func (r *ResourceCacheRegistry) Register(manager *ResourceCache) error {
	if r == nil || manager == nil {
		return ErrResourceCacheRegistryUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.closed {
		return ErrResourceCacheClosed
	}
	if manager.registry != nil {
		if manager.registry == r {
			return nil
		}
		return ErrResourceCacheAlreadyRegistered
	}
	r.managers = append(r.managers, nil)
	copy(r.managers[1:], r.managers[:len(r.managers)-1])
	r.managers[0] = manager
	manager.registry = r
	return nil
}

func (r *ResourceCacheRegistry) EvictForMemory(required *int32) (uint32, error) {
	if r == nil || required == nil {
		return 0, ErrResourceCacheRegistryUnavailable
	}
	for {
		r.mu.Lock()
		managers := append([]*ResourceCache(nil), r.managers...)
		r.mu.Unlock()
		var selected *ResourceCache
		slot := -1
		oldest := int32(0x7fffffff)
		for _, manager := range managers {
			manager.mu.Lock()
			if !manager.closed {
				for i := range manager.slots {
					entry := &manager.slots[i]
					stamp := int32(entry.stamp)
					if entry.references == 0 && stamp < oldest {
						selected = manager
						slot = i
						oldest = stamp
					}
				}
			}
			manager.mu.Unlock()
		}
		if selected == nil {
			return 0, nil
		}
		selected.mu.Lock()
		if selected.closed || slot < 0 || slot >= len(selected.slots) || selected.slots[slot].references != 0 || int32(selected.slots[slot].stamp) != oldest {
			selected.mu.Unlock()
			continue
		}
		index := selected.slots[slot].index
		if selected.slots[slot].flags != 0 {
			selected.mu.Unlock()
			return 0, ErrResourceWritebackUnsupported
		}
		size := uint32(len(selected.slots[slot].data))
		selected.mu.Unlock()
		if err := selected.Evict(index); err != nil {
			if errors.Is(err, ErrResourceInUse) || errors.Is(err, ErrResourceNotCached) || errors.Is(err, ErrResourceCacheClosed) {
				continue
			}
			return 0, err
		}
		*required -= int32(size)
		if *required < 1 {
			return 0, nil
		}
	}
}

func (r *ResourceCacheRegistry) unregister(manager *ResourceCache) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, entry := range r.managers {
		if entry == manager {
			copy(r.managers[i:], r.managers[i+1:])
			r.managers[len(r.managers)-1] = nil
			r.managers = r.managers[:len(r.managers)-1]
			return
		}
	}
}
