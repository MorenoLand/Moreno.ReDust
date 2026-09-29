package render

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"

	"redust/assets"
)

const puppetFrameSlotCount = 11

// PuppetSlotBackdrop is slot 0, the one slot that holds a full-viewport matte rather than a
// body part. It is replaced on every cue rather than persisting, and over a scene it is not
// drawn at all — the scene shows through it. Slots 1 and up are body parts.
const PuppetSlotBackdrop = 0
const puppetFrameSlotSize = 0x106
const puppetFrameTableHeaderSize = 0x16
const puppetFrameResourceListOffset = 0x1c
const puppetFrameWidthLimit = 512
const puppetViewportHeight = 264
const puppetCueRowSize = 0x52
const puppetCueSlotOffset = 0x10
const puppetPaletteOffset = 0x3a
const puppetPaletteEntrySize = 8

var puppetRasterBase [puppetFrameWidthLimit]byte

type PuppetFrame struct {
	Width  int
	Height int
	Origin image.Point
	Rows   [][]byte
}

type Puppet struct {
	cache *assets.ResourceCache
	slots [puppetFrameSlotCount][]uint32
	cues  map[uint32][]PuppetCueRow
}

type PuppetCueSlot struct {
	Frame    int16
	Position image.Point
}

type PuppetCueRow struct {
	Bounds    image.Rectangle
	HasBounds bool
	Slots     [puppetFrameSlotCount]PuppetCueSlot
}

type PuppetCanvas struct {
	frame IndexedFrame
	base  IndexedFrame
	clip  image.Rectangle
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
	puppet := &Puppet{cache: cache, cues: map[uint32][]PuppetCueRow{}}
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
	puppetFrame, err := DecodePuppetFrame(data)
	if err == nil {
		puppetFrame.Origin.X, puppetFrame.Origin.Y = puppetFrame.Origin.Y, puppetFrame.Origin.X
	}
	return puppetFrame, err
}

func (p *Puppet) Resource(index uint32) ([]byte, error) {
	if p == nil || p.cache == nil {
		return nil, fmt.Errorf("puppet is closed")
	}
	lease, err := p.cache.Acquire(index)
	if err != nil {
		return nil, err
	}
	data, err := lease.Bytes()
	if closeErr := lease.Close(); err == nil {
		err = closeErr
	}
	return data, err
}

func (p *Puppet) Palette() (color.Palette, error) {
	data, err := p.Resource(0)
	if err != nil {
		return nil, err
	}
	if len(data) < puppetPaletteOffset+256*puppetPaletteEntrySize {
		return nil, fmt.Errorf("puppet resource 0 has %d bytes, want at least %d for its CLUT", len(data), puppetPaletteOffset+256*puppetPaletteEntrySize)
	}
	palette := make(color.Palette, 256)
	for index := range palette {
		offset := puppetPaletteOffset + index*puppetPaletteEntrySize
		palette[index] = color.RGBA{R: data[offset+3], G: data[offset+5], B: data[offset+7], A: 255}
	}
	palette[0], palette[255] = color.RGBA{A: 255}, color.RGBA{R: 255, G: 255, B: 255, A: 255}
	return palette, nil
}

func (p *Puppet) CueTimeline(resource uint32) ([]PuppetCueRow, error) {
	if p == nil || p.cache == nil {
		return nil, fmt.Errorf("puppet is closed")
	}
	if timeline, found := p.cues[resource]; found {
		return timeline, nil
	}
	header, err := p.cache.Header()
	if err != nil {
		return nil, err
	}
	if resource >= header.CountB {
		return nil, fmt.Errorf("puppet cue resource %d is outside %d entries", resource, header.CountB)
	}
	data, err := p.Resource(resource)
	if err != nil {
		return nil, err
	}
	if len(data)%puppetCueRowSize != 0 {
		return nil, fmt.Errorf("puppet cue resource %d has %d bytes, not a multiple of %#x", resource, len(data), puppetCueRowSize)
	}
	timeline := make([]PuppetCueRow, len(data)/puppetCueRowSize)
	for frame := range timeline {
		row := data[frame*puppetCueRowSize : (frame+1)*puppetCueRowSize]
		y0, x0 := int(int16(binary.LittleEndian.Uint16(row[8:10]))), int(int16(binary.LittleEndian.Uint16(row[10:12])))
		y1, x1 := int(int16(binary.LittleEndian.Uint16(row[12:14]))), int(int16(binary.LittleEndian.Uint16(row[14:16])))
		if y1 > y0 && x1 > x0 {
			timeline[frame].Bounds, timeline[frame].HasBounds = image.Rect(x0, y0, x1, y1), true
		}
		for slot := range timeline[frame].Slots {
			offset := puppetCueSlotOffset + slot*6
			timeline[frame].Slots[slot] = PuppetCueSlot{Frame: int16(binary.LittleEndian.Uint16(row[offset : offset+2])), Position: image.Pt(int(int16(binary.LittleEndian.Uint16(row[offset+4:offset+6]))), int(int16(binary.LittleEndian.Uint16(row[offset+2:offset+4]))))}
		}
	}
	p.cues[resource] = timeline
	return timeline, nil
}

func (p *Puppet) DrawCue(canvas *PuppetCanvas, cue PuppetCueRow) error {
	return p.drawCue(canvas, cue, false)
}

func (p *Puppet) DrawCueOverScene(canvas *PuppetCanvas, cue PuppetCueRow) error {
	return p.drawCue(canvas, cue, true)
}

func (p *Puppet) drawCue(canvas *PuppetCanvas, cue PuppetCueRow, sceneBackground bool) error {
	if p == nil || p.cache == nil {
		return fmt.Errorf("puppet is closed")
	}
	if canvas == nil {
		return fmt.Errorf("puppet canvas is unavailable")
	}
	for slot, cueSlot := range cue.Slots {
		if cueSlot.Frame < 0 {
			if slot == 0 {
				if sceneBackground {
					canvas.RestoreBase(canvas.clip)
				} else {
					canvas.Clear(0)
				}
			}
			continue
		}
		frameCount := p.FrameCount(slot)
		if frameCount == 0 {
			continue
		}
		if int(cueSlot.Frame) >= frameCount {
			return fmt.Errorf("puppet cue slot %d frame %d is outside %d frames", slot, cueSlot.Frame, frameCount)
		}
		frame, err := p.Frame(slot, int(cueSlot.Frame))
		if err != nil {
			return fmt.Errorf("load puppet cue slot %d frame %d: %w", slot, cueSlot.Frame, err)
		}
		if sceneBackground && slot == 0 && isPuppetMatteFrame(frame) {
			continue
		}
		if err := canvas.Draw(frame, cueSlot.Position); err != nil {
			return fmt.Errorf("draw puppet cue slot %d frame %d: %w", slot, cueSlot.Frame, err)
		}
	}
	return nil
}

func isPuppetMatteFrame(frame PuppetFrame) bool {
	if frame.Width != 512 || frame.Height != puppetViewportHeight || len(frame.Rows) != frame.Height {
		return false
	}
	matte, found := byte(0), false
	for y, row := range frame.Rows {
		width, position := 0, 0
		for width < frame.Width {
			if position >= len(row) {
				return false
			}
			command := row[position]
			position++
			run, operation := int(command>>2), command&3
			if run < 1 || run > frame.Width-width {
				return false
			}
			if y == 0 {
				if operation != 2 || position >= len(row) {
					return false
				}
				if found && row[position] != matte {
					return false
				}
				matte, found = row[position], true
				position++
			} else if operation != 0 {
				return false
			}
			width += run
		}
		if position != len(row) {
			return false
		}
	}
	return found
}

func (p *Puppet) ChoiceFrame(background IndexedFrame, panelResource uint32, choices []string) (IndexedFrame, error) {
	palette, err := p.Palette()
	if err != nil {
		return IndexedFrame{}, fmt.Errorf("load puppet CLUT: %w", err)
	}
	background.Palette = palette
	canvas, err := NewPuppetCanvas(background)
	if err != nil {
		return IndexedFrame{}, err
	}
	if err := p.DrawResource(canvas, panelResource, image.Pt(0x100, 0x144)); err != nil {
		return IndexedFrame{}, fmt.Errorf("draw puppet choice panel resource %d: %w", panelResource, err)
	}
	return DrawNativePuppetChoices(canvas.Frame(), choices)
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

func CompositePuppetFrame(background IndexedFrame, frame PuppetFrame, anchor image.Point) (IndexedFrame, error) {
	canvas, err := NewPuppetCanvas(background)
	if err != nil {
		return IndexedFrame{}, err
	}
	if err := canvas.Draw(frame, anchor); err != nil {
		return IndexedFrame{}, err
	}
	return canvas.Frame(), nil
}

func HitTestPuppetFrame(frame PuppetFrame, anchor, point image.Point) (bool, error) {
	if frame.Width < 1 || frame.Height < 1 {
		return false, fmt.Errorf("invalid puppet frame dimensions %dx%d", frame.Width, frame.Height)
	}
	top := image.Pt(anchor.X-frame.Origin.X, anchor.Y-frame.Origin.Y)
	x, y := point.X-top.X, point.Y-top.Y
	if x < 0 || y < 0 || x >= frame.Width || y >= frame.Height {
		return false, nil
	}
	_, mask, err := decodeWorldActorFrame(frame)
	if err != nil {
		return false, err
	}
	return mask[y*frame.Width+x], nil
}

func (p *Puppet) DrawResource(canvas *PuppetCanvas, resource uint32, anchor image.Point) error {
	if canvas == nil {
		return fmt.Errorf("puppet canvas is unavailable")
	}
	data, err := p.Resource(resource)
	if err != nil {
		return err
	}
	frame, err := DecodePuppetFrame(data)
	if err != nil {
		return fmt.Errorf("decode puppet resource %d: %w", resource, err)
	}
	frame.Origin.X, frame.Origin.Y = frame.Origin.Y, frame.Origin.X
	return canvas.Draw(frame, anchor)
}

// # The resting pose
//
// A conversation shows its puppet in a **neutral pose** whenever nobody is speaking — while
// a choice list is up, and for a conversation that has no scripted speech at all.
//
// The pose is not a separate asset. It is **row 0 of the cue table**, which is the pose the
// line *starts* from. Measured on `LEROY.PUP`, most lines open with frame 0 in every active
// slot:
//
//	[0]=0 [1]=0 [2]=0 [3]=0 [4]=0 [5]=0 [6]=0 [7]=-1 ...
//
// but **ten of the sixty-odd lines open in a different pose** — one starts with slot 8 at
// frame 7, another with slot 9 at frame 3, another with slot 1 at frame 5. So row 0 is each
// line's own starting pose and **not one global neutral**, which is why the resting frame
// must be built from the conversation's *own* first line rather than from any line.
//
// This exists because the conversation background deliberately leaves the speaker's world
// sprite out — the dialogue draws the puppet in its place — so without the resting pose
// every state that is not mid-line showed **nobody at all**.
const (
	// PuppetRestingCueRow is the cue row that holds the starting pose, which is what a
	// conversation shows when nobody is speaking.
	PuppetRestingCueRow = 0
	// PuppetRestingFrame is the frame most lines name for every slot in row 0. It is the
	// common case, **not** a requirement: ten lines name something else there.
	PuppetRestingFrame = 0
	// PuppetBackdropWidth and PuppetBackdropHeight are the backdrop matte's dimensions, and
	// the height is also the puppet viewport's.
	PuppetBackdropWidth  = 512
	PuppetBackdropHeight = 264
)

// RestingFrame composites a puppet's starting pose over a background, using the cue
// resource of one of the conversation's own speech lines. The result is what the
// conversation shows when nobody is speaking, and a line that starts later draws over it.
func (p *Puppet) RestingFrame(background IndexedFrame, cueResource uint32) (IndexedFrame, error) {
	if p == nil || p.cache == nil {
		return IndexedFrame{}, fmt.Errorf("puppet is closed")
	}
	palette, err := p.Palette()
	if err != nil {
		return IndexedFrame{}, fmt.Errorf("load puppet CLUT: %w", err)
	}
	cues, err := p.CueTimeline(cueResource)
	if err != nil {
		return IndexedFrame{}, fmt.Errorf("load puppet cue resource %d: %w", cueResource, err)
	}
	if len(cues) == 0 {
		return IndexedFrame{}, fmt.Errorf("puppet cue resource %d has no rows", cueResource)
	}
	resting := cues[PuppetRestingCueRow]
	// Row 0 must name at least one body slot, or there is no figure to composite and the
	// caller would get back the bare scene — which is the bug this exists to fix. The
	// backdrop alone does not count, because it is a matte that is not drawn over a scene.
	body := 0
	for slot, cueSlot := range resting.Slots {
		if slot != PuppetSlotBackdrop && cueSlot.Frame >= 0 {
			body++
		}
	}
	if body == 0 {
		return IndexedFrame{}, fmt.Errorf("puppet cue resource %d row 0 names no body slot, so it holds no standing figure", cueResource)
	}
	background.Palette = palette
	canvas, err := NewPuppetCanvas(background)
	if err != nil {
		return IndexedFrame{}, err
	}
	canvas.SetClip(image.Rect(0, 0, background.Width, min(background.Height, PuppetBackdropHeight)))
	if err := p.DrawCueOverScene(canvas, resting); err != nil {
		return IndexedFrame{}, err
	}
	return canvas.Frame(), nil
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
	if background.rgba != nil && len(background.Palette) != 256 {
		return nil, fmt.Errorf("puppet background palette has %d entries, want 256", len(background.Palette))
	}
	base := background
	base.Pixels = append([]byte(nil), background.Pixels...)
	if background.rgba != nil {
		base.rgba = clonePuppetRGBA(background.rgba)
	}
	frame := base
	frame.Pixels = append([]byte(nil), base.Pixels...)
	if base.rgba != nil {
		frame.rgba = clonePuppetRGBA(base.rgba)
	}
	return &PuppetCanvas{frame: frame, base: base, clip: image.Rect(0, 0, frame.Width, frame.Height)}, nil
}

func (c *PuppetCanvas) SetClip(clip image.Rectangle) {
	if c != nil {
		c.clip = image.Rect(0, 0, c.frame.Width, c.frame.Height).Intersect(clip)
	}
}

func (c *PuppetCanvas) RestoreBase(region image.Rectangle) {
	if c == nil {
		return
	}
	region = c.clip.Intersect(region).Intersect(image.Rect(0, 0, c.frame.Width, c.frame.Height))
	for y := region.Min.Y; y < region.Max.Y; y++ {
		start, end := y*c.frame.Width+region.Min.X, y*c.frame.Width+region.Max.X
		copy(c.frame.Pixels[start:end], c.base.Pixels[start:end])
		if c.frame.rgba != nil {
			for x := region.Min.X; x < region.Max.X; x++ {
				if c.base.rgba != nil {
					source, destination := c.base.rgba.PixOffset(x, y), c.frame.rgba.PixOffset(x, y)
					copy(c.frame.rgba.Pix[destination:destination+4], c.base.rgba.Pix[source:source+4])
				} else if len(c.base.Palette) == 256 {
					value := color.RGBAModel.Convert(c.base.Palette[c.base.Pixels[y*c.frame.Width+x]]).(color.RGBA)
					index := c.frame.rgba.PixOffset(x, y)
					c.frame.rgba.Pix[index], c.frame.rgba.Pix[index+1], c.frame.rgba.Pix[index+2], c.frame.rgba.Pix[index+3] = value.R, value.G, value.B, value.A
				}
			}
		}
	}
}

func (c *PuppetCanvas) Fill(index byte) {
	if c == nil {
		return
	}
	for y := c.clip.Min.Y; y < c.clip.Max.Y; y++ {
		for x := c.clip.Min.X; x < c.clip.Max.X; x++ {
			c.frame.Pixels[y*c.frame.Width+x] = index
			c.setRGBA(image.Pt(x, y), index)
		}
	}
}

func (c *PuppetCanvas) Clear(index byte) {
	if c == nil {
		return
	}
	clip := c.clip
	c.SetClip(image.Rect(0, 0, c.frame.Width, min(c.frame.Height, puppetViewportHeight)))
	c.Fill(index)
	c.SetClip(clip)
}

func clonePuppetRGBA(source *image.RGBA) *image.RGBA {
	copy := image.NewRGBA(source.Bounds())
	copy.Pix = append(copy.Pix[:0], source.Pix...)
	return copy
}

func (c *PuppetCanvas) Draw(frame PuppetFrame, anchor image.Point) error {
	if c == nil || c.frame.Width < 1 || len(c.frame.Pixels) != c.frame.Width*c.frame.Height {
		return fmt.Errorf("puppet canvas is unavailable")
	}
	top := image.Pt(anchor.X-frame.Origin.X, anchor.Y-frame.Origin.Y)
	firstVisibleRow := 0
	if top.Y < c.clip.Min.Y {
		firstVisibleRow = min(c.clip.Min.Y-top.Y, len(frame.Rows))
	}
	for sourceY := 0; sourceY < firstVisibleRow; sourceY++ {
		if err := c.drawPuppetRow(frame.Width, frame.Rows[sourceY], top.X, top.Y+sourceY, puppetRasterBase[:]); err != nil {
			return fmt.Errorf("draw puppet scanline %d: %w", sourceY, err)
		}
	}
	for sourceY := firstVisibleRow; sourceY < len(frame.Rows); sourceY++ {
		y := top.Y + sourceY
		if y >= c.clip.Max.Y || y >= c.frame.Height {
			break
		}
		source := puppetRasterBase[:]
		if sourceY > firstVisibleRow {
			source = make([]byte, frame.Width)
			for x := range source {
				previousX := top.X + x
				if previousX >= 0 && previousX < c.frame.Width {
					source[x] = c.frame.Pixels[(y-1)*c.frame.Width+previousX]
				}
			}
		}
		if err := c.drawPuppetRow(frame.Width, frame.Rows[sourceY], top.X, y, source); err != nil {
			return fmt.Errorf("draw puppet scanline %d: %w", sourceY, err)
		}
	}
	return nil
}

func (c *PuppetCanvas) drawPuppetRow(width int, encoded []byte, left, y int, source []byte) error {
	if width < 1 {
		return fmt.Errorf("scanline width %d is invalid", width)
	}
	if len(source) < width {
		return fmt.Errorf("scanline source has %d pixels, want %d", len(source), width)
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
			var value byte
			switch operation {
			case 0:
				value = source[x+i]
			case 1:
				continue
			case 2:
				value = repeated
			case 3:
				value = encoded[position+i]
			}
			if y < c.clip.Min.Y && (operation == 2 || operation == 3) {
				puppetRasterBase[x+i] = value
			}
			point := image.Pt(destinationX, y)
			if destinationX >= 0 && destinationX < c.frame.Width && y >= 0 && y < c.frame.Height && point.In(c.clip) {
				destination := y*c.frame.Width + destinationX
				c.frame.Pixels[destination] = value
				c.setRGBA(point, value)
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
	if c.frame.rgba != nil {
		frame.rgba = clonePuppetRGBA(c.frame.rgba)
	}
	return frame
}

func (c *PuppetCanvas) setRGBA(point image.Point, index byte) {
	if c.frame.rgba == nil || !point.In(c.frame.rgba.Bounds()) {
		return
	}
	value := color.RGBAModel.Convert(c.frame.Palette[index]).(color.RGBA)
	indexOffset := c.frame.rgba.PixOffset(point.X, point.Y)
	c.frame.rgba.Pix[indexOffset], c.frame.rgba.Pix[indexOffset+1], c.frame.rgba.Pix[indexOffset+2], c.frame.rgba.Pix[indexOffset+3] = value.R, value.G, value.B, value.A
}
