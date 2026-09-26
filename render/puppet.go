package render

import (
	"encoding/binary"
	"fmt"
	"image"

	"redust/assets"
)

const puppetFrameSlotCount = 11
const puppetFrameSlotSize = 0x106
const puppetFrameTableHeaderSize = 0x16
const puppetFrameResourceListOffset = 0x1c
const puppetFrameWidthLimit = 512

type PuppetFrame struct {
	Width  int
	Height int
	Origin image.Point
	Rows   [][]byte
}

type Puppet struct {
	cache *assets.ResourceCache
	slots [puppetFrameSlotCount][]uint32
}

type PuppetCanvas struct {
	frame      IndexedFrame
	background []byte
}

func OpenPuppet(workspace assets.Workspace, name string) (*Puppet, error) {
	cache, err := workspace.OpenResourceCache(name)
	if err != nil {
		return nil, err
	}
	failed := true
	defer func() {
		if failed {
			_ = cache.Close()
		}
	}()
	header, err := cache.Header()
	if err != nil {
		return nil, err
	}
	if header.CountB < 4 {
		return nil, fmt.Errorf("puppet %q lacks its resource-3 frame table", name)
	}
	lease, err := cache.Acquire(3)
	if err != nil {
		return nil, err
	}
	table, err := lease.Bytes()
	if closeErr := lease.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	if len(table) < puppetFrameTableHeaderSize+puppetFrameSlotCount*puppetFrameSlotSize {
		return nil, fmt.Errorf("puppet %q frame table is truncated", name)
	}
	puppet := &Puppet{cache: cache}
	for slot := range puppet.slots {
		slotBase := slot * puppetFrameSlotSize
		countOffset := slotBase + puppetFrameTableHeaderSize
		resourceOffset := slotBase + puppetFrameResourceListOffset
		count := int(binary.LittleEndian.Uint16(table[countOffset : countOffset+2]))
		if count > 0x20 || resourceOffset+count*4 > len(table) {
			return nil, fmt.Errorf("puppet %q slot %d frame count %d is invalid", name, slot, count)
		}
		puppet.slots[slot] = make([]uint32, count)
		for frame := range puppet.slots[slot] {
			offset := resourceOffset + frame*4
			resource := binary.LittleEndian.Uint32(table[offset : offset+4])
			if resource >= header.CountB {
				return nil, fmt.Errorf("puppet %q slot %d frame %d references resource %d outside %d entries", name, slot, frame, resource, header.CountB)
			}
			puppet.slots[slot][frame] = resource
		}
	}
	failed = false
	return puppet, nil
}

func (p *Puppet) FrameCount(slot int) int {
	if p == nil || p.cache == nil || slot < 0 || slot >= len(p.slots) {
		return 0
	}
	return len(p.slots[slot])
}

func (p *Puppet) Frame(slot, frame int) (PuppetFrame, error) {
	if p == nil || p.cache == nil {
		return PuppetFrame{}, fmt.Errorf("puppet is closed")
	}
	if slot < 0 || slot >= len(p.slots) || frame < 0 || frame >= len(p.slots[slot]) {
		return PuppetFrame{}, fmt.Errorf("puppet frame %d/%d is outside its slots", slot, frame)
	}
	lease, err := p.cache.Acquire(p.slots[slot][frame])
	if err != nil {
		return PuppetFrame{}, err
	}
	data, err := lease.Bytes()
	if closeErr := lease.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return PuppetFrame{}, err
	}
	return DecodePuppetFrame(data)
}

func (p *Puppet) Draw(canvas *PuppetCanvas, slot, frame int, anchor image.Point) error {
	if canvas == nil {
		return fmt.Errorf("puppet canvas is unavailable")
	}
	puppetFrame, err := p.Frame(slot, frame)
	if err != nil {
		return err
	}
	return canvas.Draw(puppetFrame, anchor)
}

func (p *Puppet) Close() error {
	if p == nil || p.cache == nil {
		return nil
	}
	cache := p.cache
	p.cache = nil
	return cache.Close()
}

func DecodePuppetFrame(data []byte) (PuppetFrame, error) {
	if len(data) < 8 {
		return PuppetFrame{}, fmt.Errorf("puppet frame header is truncated")
	}
	height, width := int(binary.LittleEndian.Uint16(data[0:2])), int(binary.LittleEndian.Uint16(data[2:4]))
	if width < 1 || width > puppetFrameWidthLimit || height < 1 {
		return PuppetFrame{}, fmt.Errorf("puppet frame dimensions %dx%d are invalid", width, height)
	}
	frame := PuppetFrame{Width: width, Height: height, Origin: image.Pt(int(int16(binary.LittleEndian.Uint16(data[4:6]))), int(int16(binary.LittleEndian.Uint16(data[6:8])))), Rows: make([][]byte, height)}
	position := 8
	for row := range frame.Rows {
		if position+2 > len(data) {
			return PuppetFrame{}, fmt.Errorf("puppet scanline %d has no byte count", row)
		}
		length := int(binary.LittleEndian.Uint16(data[position : position+2]))
		position += 2
		if length > len(data)-position {
			return PuppetFrame{}, fmt.Errorf("puppet scanline %d length %d exceeds %d remaining bytes", row, length, len(data)-position)
		}
		frame.Rows[row] = append([]byte(nil), data[position:position+length]...)
		position += length
	}
	return frame, nil
}

func NewPuppetCanvas(background IndexedFrame) (*PuppetCanvas, error) {
	if background.Width < 1 || background.Height < 1 || len(background.Pixels) != background.Width*background.Height {
		return nil, fmt.Errorf("puppet background dimensions do not match its indexed pixels")
	}
	frame := background
	frame.Pixels = append([]byte(nil), background.Pixels...)
	return &PuppetCanvas{frame: frame, background: append([]byte(nil), background.Pixels...)}, nil
}

func (c *PuppetCanvas) Draw(frame PuppetFrame, anchor image.Point) error {
	if c == nil || c.frame.Width < 1 || len(c.frame.Pixels) != len(c.background) {
		return fmt.Errorf("puppet canvas is unavailable")
	}
	top := image.Pt(anchor.X-frame.Origin.X, anchor.Y-frame.Origin.Y)
	for sourceY, encoded := range frame.Rows {
		if err := c.drawPuppetRow(frame.Width, encoded, top.X, top.Y+sourceY); err != nil {
			return fmt.Errorf("draw puppet scanline %d: %w", sourceY, err)
		}
	}
	return nil
}

func (c *PuppetCanvas) drawPuppetRow(width int, encoded []byte, left, y int) error {
	if width < 1 {
		return fmt.Errorf("scanline width %d is invalid", width)
	}
	x, position := 0, 0
	for x < width {
		if position >= len(encoded) {
			return fmt.Errorf("scanline ended at pixel %d of %d", x, width)
		}
		command := encoded[position]
		position++
		run := int(command >> 2)
		if run == 0 || run > width-x {
			return fmt.Errorf("run length %d at pixel %d exceeds width %d", run, x, width)
		}
		operation := command & 3
		var repeated byte
		if operation == 2 {
			if position >= len(encoded) {
				return fmt.Errorf("repeat run at pixel %d has no color", x)
			}
			repeated = encoded[position]
			position++
		}
		if operation == 3 && run > len(encoded)-position {
			return fmt.Errorf("literal run at pixel %d exceeds scanline data", x)
		}
		for i := 0; i < run; i++ {
			destinationX := left + x + i
			if destinationX < 0 || destinationX >= c.frame.Width || y < 0 || y >= c.frame.Height {
				continue
			}
			destination := y*c.frame.Width + destinationX
			switch operation {
			case 0:
				c.frame.Pixels[destination] = c.background[destination]
			case 1:
			case 2:
				c.frame.Pixels[destination] = repeated
			case 3:
				c.frame.Pixels[destination] = encoded[position+i]
			}
		}
		if operation == 3 {
			position += run
		}
		x += run
	}
	return nil
}

func (c *PuppetCanvas) Frame() IndexedFrame {
	if c == nil {
		return IndexedFrame{}
	}
	frame := c.frame
	frame.Pixels = append([]byte(nil), c.frame.Pixels...)
	return frame
}
