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
	memory   NativeMemoryStatus
}

// The load gate's constants. nativeLoadHeadroom is the 20000 bytes FUN_004014F0
// adds to a record's length before it asks the gate (FUN_00401120). The other two
// are the gate's own: it refuses nothing while the free reading is more than
// nativeGateShortfall bytes above the request, and otherwise asks the eviction walk
// for the request minus the reading plus nativeGateHeadroom bytes.
const (
	nativeLoadHeadroom  = 20000
	nativeGateShortfall = 300000
	nativeGateHeadroom  = 500000
)

// SetMemoryStatus installs the host's free-memory reading (FUN_0042C420's three
// values) that the load gate takes its minimum from. A nil reading turns the gate
// off, so no load is refused and no row is evicted for memory.
func (r *ResourceCacheRegistry) SetMemoryStatus(status NativeMemoryStatus) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.memory = status
}

// reserveForLoad is FUN_00401120 for a request of request bytes. It returns nil when
// the load may proceed. It returns ErrResourceMemoryShortfall when the eviction walk
// ends with the request still uncovered, which is the flag FUN_004014F0 turns into
// status 0x66. A walk error is returned as it is, as FUN_00401120 returns the walk's
// status. The second-level hook DAT_00442010 is not installed by the port, so the
// walk is the only eviction (see the ledger's 2026-10-11 resource-diagnostics entry).
//
// The comparison is done in 64-bit arithmetic. FUN_00401120 does it in 32-bit int.
// A 32-bit subtraction wraps a reading above 2^31 to a positive value, so the gate
// would take the eviction path with plenty of free memory. The port's 64-bit host
// reads above 2^31 on ordinary machines (the boot cast load measured 3640623104
// bytes). The exports do not show that the native reading stays below 2^31 (that is
// an inference from its 32-bit address space, unverified). Below 2^31 the two agree.
func (r *ResourceCacheRegistry) reserveForLoad(request int64) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	status := r.memory
	r.mu.Unlock()
	if status == nil {
		return nil
	}
	pageFile, virtual, totalPhysical := status()
	free := int64(NativeAvailableMemory(pageFile, virtual, totalPhysical))
	short := request - free
	if short+nativeGateShortfall < 0 {
		return nil
	}
	need := short + nativeGateHeadroom
	if _, err := r.EvictForMemory(&need); err != nil {
		return err
	}
	if need >= 0 {
		return ErrResourceMemoryShortfall
	}
	return nil
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

// RowCounts walks the managers the way FUN_00416F10 walks the APPL manager
// list. It returns the number of managers and, over every cached row of every
// manager, how many rows have a positive reference count and how many are at
// zero or below (the native test is "< 1"). Lock order matches Register: the
// registry, then each manager.
func (r *ResourceCacheRegistry) RowCounts() (managers, referenced, unreferenced int) {
	if r == nil {
		return 0, 0, 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, manager := range r.managers {
		manager.mu.Lock()
		managers++
		for i := range manager.slots {
			if manager.slots[i].references >= 1 {
				referenced++
			} else {
				unreferenced++
			}
		}
		manager.mu.Unlock()
	}
	return managers, referenced, unreferenced
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

func (r *ResourceCacheRegistry) EvictForMemory(required *int64) (uint32, error) {
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
		*required -= int64(size)
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
