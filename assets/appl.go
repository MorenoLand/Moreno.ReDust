package assets

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
)

const (
	HeaderSize       = 0x400
	indexPageSize    = 0x200
	indexPageEntries = 0x80
	pageCacheSize    = 4
)

var ErrIndexOutOfRange = errors.New("APPL resource index is out of range")
var ErrMissingEntry = errors.New("APPL resource index has no file offset")
var ErrRecordIndexMismatch = errors.New("APPL record index does not match its table entry")

type Header struct {
	VersionTag uint32
	FileSize   uint32
	CountA     uint32
	CountB     uint32
	Magic      [8]byte
}

type indexPage struct {
	base    uint32
	stamp   uint64
	valid   bool
	offsets [indexPageEntries]uint32
}

type Container struct {
	reader io.ReaderAt
	closer io.Closer
	size   int64
	header Header
	mu     sync.Mutex
	pages  [pageCacheSize]indexPage
	clock  uint64
	closed bool
}

func NewContainer(reader io.ReaderAt, size int64) (*Container, error) {
	return newContainer(reader, size, nil)
}

func newContainer(reader io.ReaderAt, size int64, closer io.Closer) (*Container, error) {
	if reader == nil || size < HeaderSize {
		return nil, fmt.Errorf("APPL container is shorter than its %d-byte header", HeaderSize)
	}
	if uint64(size) > math.MaxUint32 {
		return nil, fmt.Errorf("APPL container exceeds the 32-bit file-size field")
	}
	var raw [HeaderSize]byte
	if _, err := io.ReadFull(io.NewSectionReader(reader, 0, HeaderSize), raw[:]); err != nil {
		return nil, fmt.Errorf("read APPL header: %w", err)
	}
	header := Header{
		VersionTag: binary.LittleEndian.Uint32(raw[0:4]),
		FileSize:   binary.LittleEndian.Uint32(raw[4:8]),
		CountA:     binary.LittleEndian.Uint32(raw[0x10:0x14]),
		CountB:     binary.LittleEndian.Uint32(raw[0x14:0x18]),
	}
	copy(header.Magic[:], raw[0x20:0x28])
	if header.VersionTag != 0x00010000 {
		return nil, fmt.Errorf("unsupported APPL header tag 0x%08x", header.VersionTag)
	}
	if header.FileSize != uint32(size) {
		return nil, fmt.Errorf("APPL stored size %d does not match file size %d", header.FileSize, size)
	}
	container := &Container{reader: reader, closer: closer, size: size, header: header}
	pages := int(header.CountA >> 7)
	if pages > pageCacheSize {
		pages = pageCacheSize
	}
	for i := 0; i < pages; i++ {
		if _, err := container.loadPage(uint32(i * indexPageEntries)); err != nil {
			return nil, err
		}
	}
	return container, nil
}

func (c *Container) Header() Header { return c.header }

func (c *Container) ReadEntry(index uint32) ([]byte, error) {
	offset, err := c.entryOffset(index)
	if err != nil {
		return nil, err
	}
	if offset == 0 {
		return nil, fmt.Errorf("resource %d: %w", index, ErrMissingEntry)
	}
	if int64(offset) > c.size-8 {
		return nil, fmt.Errorf("resource %d record header exceeds APPL file size", index)
	}
	var record [8]byte
	if _, err := io.ReadFull(io.NewSectionReader(c.reader, int64(offset), int64(len(record))), record[:]); err != nil {
		return nil, fmt.Errorf("read resource %d record header: %w", index, err)
	}
	if id := binary.LittleEndian.Uint32(record[0:4]); id != index {
		return nil, fmt.Errorf("resource %d record contains index %d: %w", index, id, ErrRecordIndexMismatch)
	}
	length := binary.LittleEndian.Uint32(record[4:8])
	dataOffset := int64(offset) + int64(len(record))
	if uint64(length) > uint64(c.size-dataOffset) {
		return nil, fmt.Errorf("resource %d payload length %d exceeds APPL file size", index, length)
	}
	data := make([]byte, int(length))
	if _, err := io.ReadFull(io.NewSectionReader(c.reader, dataOffset, int64(length)), data); err != nil {
		return nil, fmt.Errorf("read resource %d payload: %w", index, err)
	}
	return data, nil
}

func (c *Container) entryOffset(index uint32) (uint32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, errors.New("APPL container is closed")
	}
	if index >= c.header.CountB {
		return 0, fmt.Errorf("resource %d: %w", index, ErrIndexOutOfRange)
	}
	base := index &^ uint32(indexPageEntries-1)
	for i := range c.pages {
		page := &c.pages[i]
		if page.valid && page.base == base {
			c.clock++
			page.stamp = c.clock
			return page.offsets[index-base], nil
		}
	}
	page, err := c.loadPage(base)
	if err != nil {
		return 0, err
	}
	return page.offsets[index-base], nil
}

func (c *Container) loadPage(base uint32) (*indexPage, error) {
	slot := -1
	for i := range c.pages {
		if !c.pages[i].valid {
			slot = i
			break
		}
	}
	if slot < 0 {
		slot = 0
		for i := 1; i < len(c.pages); i++ {
			if c.pages[i].stamp < c.pages[slot].stamp {
				slot = i
			}
		}
	}
	offset := int64(HeaderSize) + int64(base)*4
	if offset < 0 || offset+indexPageSize > c.size {
		return nil, fmt.Errorf("APPL index page %d exceeds file size", base/indexPageEntries)
	}
	var raw [indexPageSize]byte
	if _, err := io.ReadFull(io.NewSectionReader(c.reader, offset, indexPageSize), raw[:]); err != nil {
		return nil, fmt.Errorf("read APPL index page %d: %w", base/indexPageEntries, err)
	}
	page := &c.pages[slot]
	page.base = base
	page.valid = true
	for i := range page.offsets {
		page.offsets[i] = binary.LittleEndian.Uint32(raw[i*4 : i*4+4])
	}
	c.clock++
	page.stamp = c.clock
	return page, nil
}

func (c *Container) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.closer != nil {
		return c.closer.Close()
	}
	return nil
}
