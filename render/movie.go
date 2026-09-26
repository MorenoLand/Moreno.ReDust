package render

import (
	"encoding/binary"
	"fmt"
	"math/bits"

	"redust/assets"
)

const movieDescriptorOffset = 0x8c2
const movieDescriptorSize = 0x50

type MovieFrameDescriptor struct {
	Mode          uint16
	State         uint16
	Duration      int32
	ResourceIndex uint32
	Rect          [4]int16
	Raw           [movieDescriptorSize]byte
}

type Movie struct {
	resources  *assets.ResourceCache
	frames     []MovieFrameDescriptor
	paletteRaw []byte
	defaultTick uint32
}

type MoviePlayback struct {
	movie        *Movie
	width        int
	height       int
	frame        int
	elapsed      int
	duration     int
	mode         uint16
	dibPixels    []byte
	screenPixels []byte
	palette      PaletteState
	moviePalette PaletteState
	blackPalette PaletteState
	fadeFrom     PaletteState
	fadeTo       PaletteState
	done         bool
	warning      error
}

type MoviePixels struct {
	Width    int
	Height   int
	Pitch    int
	Consumed int
	Pixels   []byte
}

var movieEntropySteps = [16]byte{8, 8, 8, 8, 8, 8, 8, 7, 6, 5, 4, 3, 2, 1, 0, 0}

func OpenMovie(workspace assets.Workspace, name string) (*Movie, error) {
	resources, err := workspace.OpenResourceCache(name)
	if err != nil {
		return nil, err
	}
	lease, err := resources.Acquire(0)
	if err != nil {
		resources.Close()
		return nil, fmt.Errorf("read movie metadata: %w", err)
	}
	data, readErr := lease.Bytes()
	closeErr := lease.Close()
	if readErr != nil || closeErr != nil {
		resources.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read movie metadata: %w", readErr)
		}
		return nil, fmt.Errorf("release movie metadata: %w", closeErr)
	}
	if len(data) < movieDescriptorOffset {
		resources.Close()
		return nil, fmt.Errorf("movie metadata is shorter than its descriptor table header")
	}
	count := int(binary.LittleEndian.Uint16(data[0x18:0x1a]))
	if count < 1 || count > (len(data)-movieDescriptorOffset)/movieDescriptorSize {
		resources.Close()
		return nil, fmt.Errorf("movie frame count %d exceeds its descriptor table", count)
	}
	header, err := resources.Header()
	if err != nil {
		resources.Close()
		return nil, err
	}
	frames := make([]MovieFrameDescriptor, count)
	for index := range frames {
		offset := movieDescriptorOffset + index*movieDescriptorSize
		raw := data[offset : offset+movieDescriptorSize]
		frames[index] = MovieFrameDescriptor{Mode: binary.LittleEndian.Uint16(raw[6:8]), State: binary.LittleEndian.Uint16(raw[8:10]), Duration: int32(binary.LittleEndian.Uint32(raw[2:6])), ResourceIndex: binary.LittleEndian.Uint32(raw[0x1c:0x20]), Rect: [4]int16{int16(binary.LittleEndian.Uint16(raw[0x28:0x2a])), int16(binary.LittleEndian.Uint16(raw[0x2a:0x2c])), int16(binary.LittleEndian.Uint16(raw[0x2c:0x2e])), int16(binary.LittleEndian.Uint16(raw[0x2e:0x30]))}}
		copy(frames[index].Raw[:], raw)
		if frames[index].ResourceIndex >= header.CountB {
			resources.Close()
			return nil, fmt.Errorf("movie frame %d references resource %d outside %d entries", index, frames[index].ResourceIndex, header.CountB)
		}
	}
	return &Movie{resources: resources, frames: frames, paletteRaw: append([]byte(nil), data[0x3e:0x83e]...), defaultTick: binary.LittleEndian.Uint32(data[0x26:0x2a])}, nil
}

func (m *Movie) FrameCount() int {
	if m == nil {
		return 0
	}
	return len(m.frames)
}

func (m *Movie) Frame(index int) (MovieFrameDescriptor, error) {
	if m == nil || m.resources == nil {
		return MovieFrameDescriptor{}, fmt.Errorf("movie is closed")
	}
	if index < 0 || index >= len(m.frames) {
		return MovieFrameDescriptor{}, fmt.Errorf("movie frame %d is out of range", index)
	}
	return m.frames[index], nil
}

func (m *Movie) FrameDuration(index int) (int, error) {
	frame, err := m.Frame(index)
	if err != nil {
		return 0, err
	}
	duration := int(frame.Duration)
	if duration < int(m.defaultTick) {
		duration = int(m.defaultTick)
	}
	if duration < 1 {
		duration = 1
	}
	return duration, nil
}

func (m *Movie) Resource(index uint32) ([]byte, error) {
	if m == nil || m.resources == nil {
		return nil, fmt.Errorf("movie is closed")
	}
	lease, err := m.resources.Acquire(index)
	if err != nil {
		return nil, err
	}
	data, readErr := lease.Bytes()
	closeErr := lease.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return data, nil
}

func (m *Movie) DecodeFrame(index int, previous []byte) (MoviePixels, error) {
	frame, err := m.Frame(index)
	if err != nil {
		return MoviePixels{}, err
	}
	data, err := m.Resource(frame.ResourceIndex)
	if err != nil {
		return MoviePixels{}, err
	}
	return DecodeMoviePixels(data, previous)
}

func (m *Movie) IndexedFrame(pixels MoviePixels) (IndexedFrame, error) {
	if m == nil || len(m.paletteRaw) != 0x800 {
		return IndexedFrame{}, fmt.Errorf("movie palette is unavailable")
	}
	return indexedFrame(pixels.Width, pixels.Height, pixels.Pixels, m.paletteRaw)
}

func BlackFrame(width, height int) (IndexedFrame, error) {
	if width <= 0 || height <= 0 {
		return IndexedFrame{}, fmt.Errorf("invalid frame dimensions %dx%d", width, height)
	}
	palette := blackPaletteState()
	return IndexedFrame{Width: width, Height: height, Pixels: make([]byte, width*height), Palette: palette.Colors()}, nil
}

func NewMoviePlayback(movie *Movie, base IndexedFrame) (*MoviePlayback, error) {
	if movie == nil || movie.resources == nil || base.Width <= 0 || base.Height <= 0 || len(base.Pixels) != base.Width*base.Height {
		return nil, fmt.Errorf("movie playback input is incomplete")
	}
	moviePalette, err := paletteStateFromRaw(movie.paletteRaw)
	if err != nil {
		return nil, err
	}
	blackPalette := blackPaletteState()
	playback := &MoviePlayback{movie: movie, width: base.Width, height: base.Height, screenPixels: append([]byte(nil), base.Pixels...), palette: blackPalette, blackPalette: blackPalette, moviePalette: moviePalette}
	if err := playback.loadFrame(0); err != nil {
		return nil, err
	}
	return playback, nil
}

func blackPaletteState() PaletteState {
	var palette PaletteState
	for index := range palette.entries {
		_ = palette.SetFixedEntry(index, 0, 0, 0)
	}
	return palette
}

func (p *MoviePlayback) CurrentFrame() IndexedFrame {
	if p == nil {
		return IndexedFrame{}
	}
	return IndexedFrame{Width: p.width, Height: p.height, Pixels: p.screenPixels, Palette: p.palette.Colors()}
}

func (p *MoviePlayback) Done() bool { return p == nil || p.done }

func (p *MoviePlayback) TakeDecodeWarning() error {
	if p == nil {
		return nil
	}
	err := p.warning
	p.warning = nil
	return err
}

func (p *MoviePlayback) Update() (IndexedFrame, bool, bool, error) {
	if p == nil || p.movie == nil {
		return IndexedFrame{}, false, true, fmt.Errorf("movie playback is unavailable")
	}
	if p.done {
		return p.CurrentFrame(), false, true, nil
	}
	changed := false
	if p.mode == 17 || p.mode == 18 {
		p.palette = interpolatePalette(p.fadeFrom, p.fadeTo, p.elapsed, p.duration-1)
		changed = true
	}
	p.elapsed++
	if p.elapsed < p.duration {
		return p.CurrentFrame(), changed, false, nil
	}
	if p.frame+1 >= p.movie.FrameCount() {
		p.done = true
		return p.CurrentFrame(), true, true, nil
	}
	if err := p.loadFrame(p.frame + 1); err != nil {
		return p.CurrentFrame(), changed, false, err
	}
	return p.CurrentFrame(), true, false, nil
}

func (p *MoviePlayback) loadFrame(index int) error {
	descriptor, err := p.movie.Frame(index)
	if err != nil {
		return err
	}
	if descriptor.Mode != 16 && descriptor.Mode != 17 && descriptor.Mode != 18 {
		return fmt.Errorf("movie frame %d uses unimplemented mode %d", index, descriptor.Mode)
	}
	duration, err := p.movie.FrameDuration(index)
	if err != nil {
		return err
	}
	pixels, decodeErr := p.movie.DecodeFrame(index, p.dibPixels)
	if len(pixels.Pixels) == 0 {
		return decodeErr
	}
	p.dibPixels = pixels.Pixels
	if decodeErr != nil {
		p.warning = decodeErr
	}
	top, left, bottom, right := int(descriptor.Rect[0]), int(descriptor.Rect[1]), int(descriptor.Rect[2]), int(descriptor.Rect[3])
	if top < 0 {
		top = 0
	}
	if left < 0 {
		left = 0
	}
	if bottom > p.height {
		bottom = p.height
	}
	if right > p.width {
		right = p.width
	}
	if bottom > top && right > left {
		for y := top; y < bottom && y < pixels.Height; y++ {
			source := y*pixels.Pitch + left
			destination := y*p.width + left
			copy(p.screenPixels[destination:destination+right-left], pixels.Pixels[source:source+right-left])
		}
	}
	p.frame, p.elapsed, p.duration, p.mode = index, 0, duration, descriptor.Mode
	if descriptor.Mode == 17 {
		p.fadeFrom, p.fadeTo = p.palette, p.blackPalette
	} else if descriptor.Mode == 18 {
		p.fadeFrom, p.fadeTo = p.palette, p.moviePalette
	} else {
		p.palette = p.moviePalette
	}
	return nil
}

func DecodeMoviePixels(data, previous []byte) (MoviePixels, error) {
	if len(data) < 4 {
		return MoviePixels{}, fmt.Errorf("movie frame payload is shorter than its dimensions")
	}
	height, width := int(binary.LittleEndian.Uint16(data[:2])), int(binary.LittleEndian.Uint16(data[2:4]))
	if height == 0 || width == 0 {
		return MoviePixels{}, fmt.Errorf("movie frame dimensions are invalid: %dx%d", width, height)
	}
	pitch := (width + 3) &^ 3
	pixels := make([]byte, pitch*height)
	if len(previous) != 0 {
		if len(previous) < len(pixels) {
			return MoviePixels{}, fmt.Errorf("previous movie frame has %d pixels, want at least %d", len(previous), len(pixels))
		}
		copy(pixels, previous[:len(pixels)])
	}
	position := 4
	destination := 0
	for row := 0; row < height; row++ {
		if position >= len(data) {
			return MoviePixels{}, fmt.Errorf("movie frame ended before row %d", row)
		}
		command := data[position]
		position++
		if command == 4 {
			if width > len(data)-position {
				return MoviePixels{}, fmt.Errorf("movie raw row %d exceeds its payload", row)
			}
			if destination < 0 || destination+width > len(pixels) {
				return MoviePixels{}, fmt.Errorf("movie raw row %d exceeds its destination", row)
			}
			copy(pixels[destination:destination+width], data[position:position+width])
			position += width
			destination += pitch
			continue
		}
		if command == 40 {
			destination += pitch
			continue
		}
		delta, ok := movieRowDelta(command)
		if !ok {
			return MoviePixels{}, fmt.Errorf("movie row %d has unsupported command 0x%02x at byte %d with destination %d", row, command, position-1, destination)
		}
		reference := destination + delta*pitch
		if command >= 44 {
			if reference < 0 || reference+width > len(pixels) {
				return MoviePixels{}, fmt.Errorf("movie row %d references pixels outside the frame", row)
			}
			copy(pixels[destination:destination+width], pixels[reference:reference+width])
			destination += pitch
			continue
		}
		var err error
		position, err = decodeMovieRuns(data, position, pixels, &destination, reference, width)
		if err != nil {
			return MoviePixels{Width: width, Height: height, Pitch: pitch, Consumed: position, Pixels: pixels}, fmt.Errorf("movie row %d: %w", row, err)
		}
		destination += pitch - width
	}
	return MoviePixels{Width: width, Height: height, Pitch: pitch, Consumed: position, Pixels: pixels}, nil
}

func movieRowDelta(command byte) (int, bool) {
	switch {
	case command >= 8 && command <= 20 && command&3 == 0:
		return int(command/4) - 6, true
	case command >= 24 && command <= 36 && command&3 == 0:
		return int(command/4) - 5, true
	case command >= 44 && command <= 56 && command&3 == 0:
		return int(command/4) - 15, true
	case command >= 60 && command <= 72 && command&3 == 0:
		return int(command/4) - 14, true
	default:
		return 0, false
	}
}

func decodeMovieRuns(data []byte, position int, pixels []byte, destinationPtr *int, reference, width int) (int, error) {
	destination := *destinationPtr
	defer func() { *destinationPtr = destination }()
	remaining := width
	trace := make([]string, 0, 12)
	for remaining > 0 {
		if position >= len(data) {
			return position, fmt.Errorf("run stream ended with %d pixels remaining", remaining)
		}
		tokenOffset := position
		token := data[position]
		position++
		length := int(token >> 3)
		if length == 0 {
			if position >= len(data) {
				return position, fmt.Errorf("extended run length is missing")
			}
			length = int(data[position]) + 32
			position++
		}
		trace = append(trace, fmt.Sprintf("%d:%02x:%d:%d", tokenOffset, token, destination, length))
		if len(trace) > 12 {
			trace = trace[1:]
		}
		destinationEnd := destination + length
		switch token & 7 {
		case 0:
			if position >= len(data) || destination < 0 || destination >= len(pixels) {
				return position, fmt.Errorf("adaptive run starts outside the frame")
			}
			pixels[destination] = data[position]
			position++
			if length > 1 {
				var err error
				position, err = decodeMovieEntropy(data, position, pixels, destination+1, destination, length-1)
				if err != nil {
					return position, fmt.Errorf("entropy seed run: %w; runs %v", err, trace)
				}
			}
		case 1:
			var err error
			position, err = decodeMovieEntropy(data, position, pixels, destination, reference, length)
			if err != nil {
				return position, fmt.Errorf("entropy run: %w; runs %v", err, trace)
			}
		case 2:
		case 3:
			if reference < 0 || reference+length > len(pixels) || destinationEnd > len(pixels) {
				return position, fmt.Errorf("reference copy leaves the frame")
			}
			copy(pixels[destination:destinationEnd], pixels[reference:reference+length])
		case 4:
			if destination == 0 || destinationEnd > len(pixels) {
				return position, fmt.Errorf("repeated pixel is unavailable")
			}
			color := pixels[destination-1]
			for i := 0; i < length; i++ {
				pixels[destination+i] = color
			}
		case 5:
			var err error
			position, remaining, err = copyMovieLiteral(data, position, pixels, destination, length, remaining)
			if err != nil {
				return position, err
			}
			destination += length
			reference += length
			continue
		case 6:
			if position >= len(data) || destinationEnd > len(pixels) {
				return position, fmt.Errorf("solid run exceeds its source or destination")
			}
			color := data[position]
			position++
			for i := 0; i < length; i++ {
				pixels[destination+i] = color
			}
		case 7:
			if position+2 > len(data) {
				return position, fmt.Errorf("back-reference distance is missing")
			}
			distance := int(binary.LittleEndian.Uint16(data[position : position+2]))
			position += 2
			source := destination - distance
			if source < 0 || destinationEnd > len(pixels) {
				return position, fmt.Errorf("back-reference distance %d at pixel %d from token 0x%02x at byte %d leaves the frame; runs %v", distance, destination, token, tokenOffset, trace)
			}
			for i := 0; i < length; i++ {
				pixels[destination+i] = pixels[source+i]
			}
		}
		destination += length
		reference += length
		remaining -= length
	}
	return position, nil
}

func copyMovieLiteral(data []byte, position int, pixels []byte, destination, length, remaining int) (int, int, error) {
	if position+length > len(data) || destination+length > len(pixels) {
		return position, remaining, fmt.Errorf("literal run exceeds its source or destination")
	}
	copy(pixels[destination:destination+length], data[position:position+length])
	return position + length, remaining - length, nil
}

func decodeMovieEntropy(data []byte, position int, pixels []byte, destination, reference, count int) (int, error) {
	if count == 0 {
		return position, nil
	}
	if position+4 > len(data) {
		return position, fmt.Errorf("entropy state at byte %d needs 4 bytes, payload has %d", position, len(data))
	}
	rangeWord := binary.BigEndian.Uint16(data[position : position+2])
	codeWord := binary.BigEndian.Uint16(data[position+2 : position+4])
	input := position + 4
	bitCount := 16
	for remaining := count; remaining > 0; remaining-- {
		msb := bits.Len16(rangeWord) - 1
		var pixel byte
		shift := 16
		switch {
		case msb < 0, msb < 8:
			base, err := moviePixelAt(pixels, reference)
			if err != nil {
				return position, err
			}
			pixel = byte(byte(rangeWord) + base)
			rangeWord = rangeWord&0xff00 | uint16(pixel)
		case msb == 15:
			var err error
			pixel, err = moviePixelAt(pixels, reference)
			if err != nil {
				return position, err
			}
			shift = 1
		default:
			index := msb - 1
			if index < 0 || index >= len(movieEntropySteps) {
				return position, fmt.Errorf("entropy context %d is invalid", index)
			}
			base, err := moviePixelAt(pixels, reference)
			if err != nil {
				return position, err
			}
			step := movieEntropySteps[index]
			if rangeWord&(1<<index) == 0 {
				pixel = base - step
			} else {
				pixel = base + step
			}
			shift = int(step) + 2
		}
		if destination < 0 || destination >= len(pixels) {
			return position, fmt.Errorf("entropy destination is outside the frame")
		}
		pixels[destination] = pixel
		destination++
		reference++
		for shift > 0 {
			if bitCount == 0 {
				if input+2 > len(data) {
					return position, fmt.Errorf("entropy word at byte %d needs 2 bytes, payload has %d, bitCount=%d shift=%d decoded=%d/%d", input, len(data), bitCount, shift, count-remaining, count)
				}
				codeWord = binary.BigEndian.Uint16(data[input : input+2])
				input += 2
				bitCount = 16
			}
			take := shift
			if take > bitCount {
				take = bitCount
			}
			rangeWord = movieShiftDouble(rangeWord, codeWord, take)
			codeWord <<= take
			bitCount -= take
			shift -= take
		}
		if bitCount == 0 && (msb != 15 || remaining > 1) {
			if input+2 > len(data) {
				return position, fmt.Errorf("entropy word at byte %d needs 2 bytes, payload has %d, bitCount=%d shift=%d decoded=%d/%d", input, len(data), bitCount, shift, count-remaining+1, count)
			}
			codeWord = binary.BigEndian.Uint16(data[input : input+2])
			input += 2
			bitCount = 16
		}
	}
	consumed := input - 2
	if bitCount >= 8 {
		consumed--
	}
	if bitCount == 16 {
		consumed--
	}
	if consumed < 0 || consumed > len(data) {
		return position, fmt.Errorf("entropy byte cursor %d is outside its stream", consumed)
	}
	return consumed, nil
}

func moviePixelAt(pixels []byte, index int) (byte, error) {
	if index < 0 || index >= len(pixels) {
		return 0, fmt.Errorf("movie reference pixel %d is outside the frame", index)
	}
	return pixels[index], nil
}

func movieShiftDouble(destination, source uint16, count int) uint16 {
	if count == 0 {
		return destination
	}
	if count == 16 {
		return source
	}
	return destination<<count | source>>(16-count)
}

func (m *Movie) Close() error {
	if m == nil || m.resources == nil {
		return nil
	}
	err := m.resources.Close()
	m.resources = nil
	return err
}
