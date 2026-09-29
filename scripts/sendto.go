package scripts

import "fmt"

// The sendto family. Every command-side sendto opcode is a one-line forwarder to
// the value-side handler of the same name with a fifth argument of zero, so the
// two dispatchers share handler bodies rather than duplicating them. All seven
// are transcribed here from live Ghidra decompilation, for example:
//
//	void FUN_00412EC0(p1, p2, p3, p4) { FUN_00412EE0(p1, p2, p3, 0, p4); }
//	void FUN_00413210(p1, p2, p3, p4) { FUN_00413230(p1, p2, p3, 0, p4); }
//	void FUN_00412A70(p1, p2, p3, p4) { FUN_00412A90(p1, p2, p3, 0, p4); }
//	void FUN_0040CB40(p1, p2, p3, p4) { FUN_0040CB60(p1, p2, p3, 0, p4); }
//	void FUN_00420CC0(p1, p2, p3, p4) { FUN_00420CE0(p1, p2, p3, 0, p4); }
//	void FUN_00421020(p1, p2, p3, p4) { FUN_00421040(p1, p2, p3, 0, p4); }
//	void FUN_0041A180(p1, p2, p3, p4) { FUN_0041A1A0(p1, p2, p3, 0, p4); }
//
// The zero fifth argument is what distinguishes the command form from the value
// form, so the mapping is recorded as data rather than derived: the two opcodes
// are 8024 to 8068 apart depending on the target, so there is no constant offset
// and a formula would be wrong.
//
// The inner handlers also pin the sendto argument syntax, which is the same
// `target , event` shape for every member of the family. From FUN_00412EE0, the
// sendtoflat inner:
//
//	if (DAT_00459A00 == 0) return 0x0B;                 // no stage
//	FUN_004220A0(..., &name, &consumed);
//	if (*(next record) != 0x0FB4) return 0x1C;          // the comma is REQUIRED
//	skip the comma; resolve the name; build the child context
//
// and identically from FUN_0041A1A0, the sendtoscene inner, except its guard is
// the set rather than the stage.

// SendToTarget names the kind of entity a sendto opcode addresses.
type SendToTarget uint8

const (
	// SendToActor addresses an actor.
	SendToActor SendToTarget = iota
	// SendToScene addresses a scene.
	SendToScene
	// SendToProp addresses a prop.
	SendToProp
	// SendToShop addresses a shop.
	SendToShop
	// SendToButton addresses a button.
	SendToButton
	// SendToFlat addresses a flat.
	SendToFlat
	// SendToStage addresses the stage itself.
	SendToStage
)

// SendToForward is one verified forwarder, command opcode to value opcode.
type SendToForward struct {
	// Target is the entity kind.
	Target SendToTarget
	// Name is the script keyword.
	Name string
	// CommandOpcode is the FUN_00424890 opcode, 12xxx.
	CommandOpcode uint16
	// CommandHandler is the native forwarder.
	CommandHandler string
	// ValueOpcode is the FUN_004137B0 opcode, 20xxx.
	ValueOpcode uint16
	// ValueHandler is the native function the forwarder calls.
	ValueHandler string
	// Guard is the context the inner handler requires before parsing.
	Guard ScriptEntryGuard
}

// sendToForwards is the verified family. Every CommandHandler and ValueHandler is
// cross-checked against the two transcribed dispatch tables by the tests, so this
// table cannot drift from either.
var sendToForwards = []SendToForward{
	{SendToActor, "sendtoactor", 12016, "FUN_0040CB40", 20084, "FUN_0040CB60", GuardNone},
	{SendToScene, "sendtoscene", 12034, "FUN_0041A180", 20085, "FUN_0041A1A0", GuardRequireSet},
	{SendToProp, "sendtoprop", 12055, "FUN_00420CC0", 20088, "FUN_00420CE0", GuardNone},
	{SendToShop, "sendtoshop", 12059, "FUN_00421020", 20089, "FUN_00421040", GuardNone},
	{SendToButton, "sendtobutton", 12068, "FUN_00412A70", 20092, "FUN_00412A90", GuardRequireStage},
	{SendToFlat, "sendtoflat", 12069, "FUN_00412EC0", 20093, "FUN_00412EE0", GuardRequireStage},
	{SendToStage, "sendtostage", 12070, "FUN_00413210", 20094, "FUN_00413230", GuardNone},
}

// SendToForwardFor returns the forwarder for a command-side sendto opcode.
func SendToForwardFor(opcode uint16) (SendToForward, bool) {
	for _, forward := range sendToForwards {
		if forward.CommandOpcode == opcode {
			return forward, true
		}
	}
	return SendToForward{}, false
}

// SendToForwardForName returns the forwarder by script keyword, which is how a
// diagnostic can name the target without a numeric opcode.
func SendToForwardForName(name string) (SendToForward, bool) {
	for _, forward := range sendToForwards {
		if forward.Name == name {
			return forward, true
		}
	}
	return SendToForward{}, false
}

// The sendto argument syntax, from the inner handlers above.
const (
	// SendToCommaKind is 0x0FB4, 4020, the "," token. The inner handlers require
	// the record immediately after the target name to be a comma, so
	// `sendtoflat(name , event)` is well formed and `sendtoflat(name event)` is
	// not. This is the same token the command call frame requires between an
	// opcode and its argument list.
	SendToCommaKind uint16 = 0x0FB4
	// StatusMissingComma is native status 0x1C, returned by every sendto inner
	// handler when the record after the target name is not the comma. It is
	// distinct from the 0x0B no-stage, 0x28 no-set and 0x0A not-found statuses,
	// so a malformed sendto is never confused with a missing target.
	StatusMissingComma uint16 = 0x1C
	// SendToRecordBytes is the record size the inner handlers advance by, from
	// `next = param_2 + consumed * 8` and `next += 4` shorts, that is 8 bytes per
	// record and 4 shorts to skip the comma.
	SendToRecordBytes = 8
)

// ParseSendToTarget validates the head of a sendto argument list: the target name
// record followed by the required comma. It reproduces the two steps every inner
// handler performs, so the shared part of the family is converted once rather
// than seven times.
//
// kinds is the record-kind stream starting at the target name. It returns the
// index of the first record after the comma, which is where the event name begins.
func ParseSendToTarget(kinds []uint16) (next int, status uint16, err error) {
	if len(kinds) == 0 {
		return 0, StatusMissingComma, fmt.Errorf("sendto has no target name record")
	}
	// Step one: the record after the name must be the comma.
	if len(kinds) < 2 {
		return 0, StatusMissingComma, fmt.Errorf("sendto target is not followed by a record to hold the comma")
	}
	if kinds[1] != SendToCommaKind {
		return 0, StatusMissingComma, fmt.Errorf("sendto expects %q after the target, found kind %d",
			",", kinds[1])
	}
	return 2, 0, nil
}

// SendToGuardStatus evaluates a forwarder's inner guard and returns the native
// status the inner handler would return, or 0 when it passes. A member with no
// guard parses regardless of the surrounding context.
func SendToGuardStatus(forward SendToForward, ctx ScriptContext) uint16 {
	switch forward.Guard {
	case GuardRequireStage:
		if !ctx.StageOpen {
			return StatusNoActiveStage
		}
	case GuardRequireSet:
		if !ctx.SetActive {
			return StatusNoActiveSet
		}
	}
	return 0
}

// ParseSendTo validates a whole sendto head: the inner guard, the target record
// and the required comma. The guard is checked first, matching the reference,
// which returns before parsing anything.
func ParseSendTo(forward SendToForward, ctx ScriptContext, kinds []uint16) (next int, status uint16, err error) {
	if status = SendToGuardStatus(forward, ctx); status != 0 {
		return 0, status, nil
	}
	return ParseSendToTarget(kinds)
}

// The reference-counted entity resource cache, from FUN_004186D0. Every file
// backed asset goes through it, so its layout is worth pinning.
//
//	if (FUN_00418A00(context, index, &slot) == 0) {        // already loaded
//	    entry = table + slot * 0x20;
//	    entry.refCount += 1;                               // at +0x10
//	    entry.stamp = DAT_00459EEC;                        // at +0x08
//	    DAT_00459EEC += 1;                                 // global stamp counter
//	    return entry.handle;                               // at +0x0C
//	}
//	FUN_004014F0(context, index, &handle);                // load it
//	FUN_00401120(GlobalSize(handle) + 20000, &buffer);     // with 20000 bytes spare
//
// So entries are 0x20, that is 32, bytes with a 32-bit stamp at offset 8, the
// handle at offset 12 and a 16-bit reference count at offset 16, and a cache hit
// takes a reference and refreshes the stamp.
const (
	// EntityResourceEntrySize is the verified cache entry stride, 32 bytes.
	EntityResourceEntrySize = 0x20
	// EntityResourceStampOffset is the 32-bit stamp field.
	EntityResourceStampOffset = 0x08
	// EntityResourceHandleOffset is the handle field.
	EntityResourceHandleOffset = 0x0C
	// EntityResourceRefCountOffset is the 16-bit reference count field.
	EntityResourceRefCountOffset = 0x10
	// EntityResourceHeadroom is the 20000 bytes the reference allocates beyond
	// the resource's own size. It is an odd, specific number and is recorded
	// rather than rounded: it is the working space the engine assumes it needs
	// beside a loaded asset.
	EntityResourceHeadroom = 20000
	// The three diagnostics the loader raises, in the order it can reach them.
	EntityResourceLoadFailedDiag   uint16 = 0x145E
	EntityResourceAllocFailedDiag  uint16 = 0x145F
	EntityResourceNullHandleDiag   uint16 = 0x1460
)

// EntityResourceEntry is one cache entry, laid out as the reference's 32 bytes.
// The Go side tracks the bookkeeping the reference does; loading a resource
// itself is the asset layer's job, so Load is injected.
//
// The key fields at offsets 0 and 4 were identified later, from FUN_00418A00, the
// slot lookup, and they are what makes the cache a map from a (context, index)
// pair to a handle rather than a list.
type EntityResourceEntry struct {
	// Context is the key field at offset 0, the owning context handle.
	Context uint32
	// Index is the key field at offset 4, the index within that context. It is
	// signed, since the lookup compares it as a signed value.
	Index int32
	// Stamp is the 32-bit field at offset 8, refreshed on every hit.
	Stamp uint32
	// Handle identifies the loaded resource, at offset 12.
	Handle uint32
	// RefCount is the 16-bit field at offset 16, incremented on every hit.
	RefCount uint16
}

// Key returns the entry's key, for pairing with FindResourceSlot.
func (e EntityResourceEntry) Key() ResourceKey {
	return ResourceKey{Context: e.Context, Index: e.Index}
}

// EntityResourceCache is the reference-counted table FUN_004186D0 maintains.
// The Go side tracks the bookkeeping the reference does; loading a resource
// itself is the asset layer's job, so Load is injected.
type EntityResourceCache struct {
	// Entries is the table, indexed by slot.
	Entries []EntityResourceEntry
	// Stamp is the global stamp counter, DAT_00459EEC. It is incremented on
	// every hit, which is what lets an eviction pass find the least recently
	// touched entry.
	Stamp uint32
	// Load fetches a resource that is not yet cached, returning its handle. It
	// returns ok with size giving the resource's own size, since the headroom is
	// added by the cache.
	Load func(index int) (handle uint32, size int, ok bool)
	// Allocate reserves a buffer of the given size, which the reference asks for
	// as the resource's size plus EntityResourceHeadroom.
	Allocate func(size int) bool
	// ContextOf supplies the context handle a newly loaded entry is keyed under,
	// since the native reads it from its own caller. It may be nil, in which case
	// entries are keyed under context zero.
	ContextOf func() uint32
}

// slotFor finds the cached slot for an index, which the reference delegates to
// FUN_00418A00. It is injected because the slot assignment policy is inside that
// function.
type EntityResourceCacheWithSlots struct {
	*EntityResourceCache
	// SlotOf returns the slot holding an index, or ok false when it is absent.
	SlotOf func(index int) (slot int, ok bool)
	// RecordSlot records a newly loaded index in a slot.
	RecordSlot func(index, slot int)
}

// Acquire takes a reference to a cached resource, reproducing the hit path. It
// increments the count, stamps the entry with the current stamp and then bumps
// the stamp, in that order, which is what the reference does. The entry is
// located by slot.
func (c *EntityResourceCache) Acquire(slot int) (EntityResourceEntry, bool) {
	if c == nil || slot < 0 || slot >= len(c.Entries) {
		return EntityResourceEntry{}, false
	}
	entry := &c.Entries[slot]
	entry.RefCount++
	entry.Stamp = c.Stamp
	c.Stamp++
	return *entry, true
}

// AcquireKey locates a cached resource by its (context, index) key, which is how
// FUN_00418A00 finds a slot, and then takes a reference to it.
func (c *EntityResourceCache) AcquireKey(key ResourceKey) (EntityResourceEntry, bool) {
	if c == nil {
		return EntityResourceEntry{}, false
	}
	keys := make([]ResourceKey, len(c.Entries))
	for i := range c.Entries {
		keys[i] = c.Entries[i].Key()
	}
	slot, found := FindResourceSlot(keys, len(c.Entries), key.Context, key.Index)
	if !found {
		return EntityResourceEntry{}, false
	}
	return c.Acquire(slot)
}

// Release drops a reference, and reports whether the entry reached zero. The
// reference decrements the same field; what it does at zero is in the eviction
// path, which is not decompiled, so this does not free anything.
func (c *EntityResourceCache) Release(slot int) (remaining uint16, reachedZero bool) {
	if c == nil || slot < 0 || slot >= len(c.Entries) {
		return 0, false
	}
	entry := &c.Entries[slot]
	if entry.RefCount == 0 {
		return 0, true
	}
	entry.RefCount--
	return entry.RefCount, entry.RefCount == 0
}

// RequiredSize returns the buffer size the reference requests for a resource of
// the given own size, which is that size plus the verified 20000 bytes of
// headroom.
func RequiredSize(resourceSize int) int {
	return resourceSize + EntityResourceHeadroom
}

// LoadSlot populates an empty slot, reproducing the miss path: load the resource,
// allocate the resource's size plus the headroom, and refuse a null allocation.
func (c *EntityResourceCache) LoadSlot(slot, index int) (EntityResourceEntry, uint16, error) {
	if c == nil || slot < 0 {
		return EntityResourceEntry{}, 0, fmt.Errorf("resource slot %d is not usable", slot)
	}
	if c.Load == nil {
		return EntityResourceEntry{}, 0, fmt.Errorf("resource loader is not available")
	}
	handle, size, ok := c.Load(index)
	if !ok {
		return EntityResourceEntry{}, EntityResourceLoadFailedDiag,
			fmt.Errorf("loading resource %d failed (FUN_0042C470(0, %#x))", index, EntityResourceLoadFailedDiag)
	}
	if c.Allocate == nil {
		return EntityResourceEntry{}, 0, fmt.Errorf("resource allocator is not available")
	}
	if !c.Allocate(RequiredSize(size)) {
		return EntityResourceEntry{}, EntityResourceAllocFailedDiag,
			fmt.Errorf("allocating %d bytes for resource %d failed (FUN_0042C470(0, %#x))",
				RequiredSize(size), index, EntityResourceAllocFailedDiag)
	}
	if handle == 0 {
		return EntityResourceEntry{}, EntityResourceNullHandleDiag,
			fmt.Errorf("resource %d loaded a null handle (FUN_0042C470(0, %#x))", index, EntityResourceNullHandleDiag)
	}
	if c.Stamp > 0 && c.Stamp != ^uint32(0) {
		c.Stamp++
	}
	entry := EntityResourceEntry{Context: c.nextContext(), Index: int32(index), Stamp: c.Stamp, Handle: handle, RefCount: 1}
	if slot < len(c.Entries) {
		c.Entries[slot] = entry
	}
	return entry, 0, nil
}

// nextContext returns the context handle a newly loaded entry records. The Go side
// has no context handles of its own, so it is supplied by the caller through
// ContextOf and defaults to zero.
func (c *EntityResourceCache) nextContext() uint32 {
	if c.ContextOf == nil {
		return 0
	}
	return c.ContextOf()
}
