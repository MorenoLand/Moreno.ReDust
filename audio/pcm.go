package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	ebitenaudio "github.com/hajimehoshi/ebiten/v2/audio"
)

type PCMFormat struct {
	SampleRate    int
	Channels      int
	BitsPerSample int
}

type NativeSoundMetadata struct {
	SampleRate        uint32
	Channels          int16
	Field24           int32
	Field24PerChannel int32
	RateMultiplier    uint8
	RateCode          uint8
	Flags             [2]uint32
	Name              []byte
}

func ParseNativeSoundMetadata(resource, name []byte) (NativeSoundMetadata, error) {
	if len(resource) < 0x28 {
		return NativeSoundMetadata{}, fmt.Errorf("native sound resource is shorter than its 0x28-byte header")
	}
	channels := int16(binary.LittleEndian.Uint16(resource[0x1a:0x1c]))
	if channels == 0 {
		return NativeSoundMetadata{}, fmt.Errorf("native sound resource has zero channels")
	}
	var multiplier, rateCode uint8
	sampleRate := binary.LittleEndian.Uint32(resource[0x1c:0x20])
	switch sampleRate {
	case 0x2b11:
		multiplier, rateCode = 1, 2
	case 0x5622:
		multiplier, rateCode = 2, 3
	case 0xac44:
		multiplier, rateCode = 4, 4
	default:
		return NativeSoundMetadata{}, fmt.Errorf("unsupported native sound rate %d", sampleRate)
	}
	if len(name) == 0 || int(name[0])+1 > len(name) {
		return NativeSoundMetadata{}, fmt.Errorf("native sound name is not a complete Pascal string")
	}
	field24 := int32(binary.LittleEndian.Uint32(resource[0x24:0x28]))
	return NativeSoundMetadata{SampleRate: sampleRate, Channels: channels, Field24: field24, Field24PerChannel: field24 / int32(channels), RateMultiplier: multiplier, RateCode: rateCode, Flags: [2]uint32{1, 1}, Name: append([]byte(nil), name[:int(name[0])+1]...)}, nil
}

func PCMFormatFromNative(rateMultiplier, bytesPerSample, channels int) (PCMFormat, error) {
	if rateMultiplier <= 0 || uint64(rateMultiplier) > uint64(^uint32(0))/0x2b11 {
		return PCMFormat{}, fmt.Errorf("invalid native audio rate multiplier %d", rateMultiplier)
	}
	sampleRate := uint64(rateMultiplier) * 0x2b11
	if sampleRate > uint64(int(^uint(0)>>1)) {
		return PCMFormat{}, fmt.Errorf("native audio sample rate %d exceeds the supported range", sampleRate)
	}
	if bytesPerSample != 1 && bytesPerSample != 2 {
		return PCMFormat{}, fmt.Errorf("unsupported native audio sample size %d", bytesPerSample)
	}
	if channels != 1 && channels != 2 {
		return PCMFormat{}, fmt.Errorf("unsupported native audio channel count %d", channels)
	}
	return PCMFormat{SampleRate: int(sampleRate), Channels: channels, BitsPerSample: bytesPerSample * 8}, nil
}

func ToStereoPCM16(input []byte, format PCMFormat) ([]byte, error) {
	if format.SampleRate <= 0 {
		return nil, fmt.Errorf("invalid sample rate %d", format.SampleRate)
	}
	if format.Channels != 1 && format.Channels != 2 {
		return nil, fmt.Errorf("unsupported PCM channel count %d", format.Channels)
	}
	if format.BitsPerSample != 8 && format.BitsPerSample != 16 {
		return nil, fmt.Errorf("unsupported PCM depth %d", format.BitsPerSample)
	}
	bytesPerSample := format.BitsPerSample / 8
	frameBytes := bytesPerSample * format.Channels
	if len(input)%frameBytes != 0 {
		return nil, fmt.Errorf("PCM byte count %d is not frame aligned", len(input))
	}
	frames := len(input) / frameBytes
	output := make([]byte, frames*4)
	readSample := func(offset int) int16 {
		if format.BitsPerSample == 8 {
			return int16(int(input[offset])-128) << 8
		}
		return int16(binary.LittleEndian.Uint16(input[offset : offset+2]))
	}
	for frame := 0; frame < frames; frame++ {
		inputOffset := frame * frameBytes
		left := readSample(inputOffset)
		right := left
		if format.Channels == 2 {
			right = readSample(inputOffset + bytesPerSample)
		}
		outputOffset := frame * 4
		binary.LittleEndian.PutUint16(output[outputOffset:outputOffset+2], uint16(left))
		binary.LittleEndian.PutUint16(output[outputOffset+2:outputOffset+4], uint16(right))
	}
	return output, nil
}

func NewPlayer(context *ebitenaudio.Context, input []byte, format PCMFormat) (*Player, error) {
	if context == nil {
		return nil, fmt.Errorf("audio context is nil")
	}
	pcm, err := stereoPCMAtRate(input, format, context.SampleRate())
	if err != nil {
		return nil, err
	}
	return newPlayer(context.NewPlayerFromBytes(pcm)), nil
}

func NewNativePlaylist(context *ebitenaudio.Context, tracks []NativeSound, events []int, loopIndex int) (*Player, error) {
	if context == nil {
		return nil, fmt.Errorf("audio context is nil")
	}
	stream, err := nativePlaylistStream(tracks, events, loopIndex, context.SampleRate())
	if err != nil || stream == nil {
		return nil, err
	}
	player, err := context.NewPlayer(stream)
	if err != nil {
		return nil, err
	}
	managed := newPlayer(player)
	managed.Play()
	return managed, nil
}

func nativePlaylistStream(tracks []NativeSound, events []int, loopIndex, targetRate int) (io.Reader, error) {
	if len(events) == 0 {
		return nil, nil
	}
	oneShot := loopIndex == -1
	if !oneShot && (loopIndex < 0 || loopIndex >= len(events)) {
		return nil, fmt.Errorf("native audio loop index %d is outside %d events", loopIndex, len(events))
	}
	prepared := make([][]byte, len(tracks))
	for index, track := range tracks {
		var err error
		prepared[index], err = stereoPCMAtRate(track.Samples, track.Format, targetRate)
		if err != nil {
			return nil, fmt.Errorf("prepare native audio track %d: %w", index, err)
		}
	}
	var prefix, loop bytes.Buffer
	if oneShot {
		loopIndex = len(events)
	}
	for index, event := range events {
		if event < 0 || event >= len(prepared) {
			return nil, fmt.Errorf("native audio event %d references track %d outside %d tracks", index, event, len(prepared))
		}
		segment := &loop
		if index < loopIndex {
			segment = &prefix
		}
		if _, err := segment.Write(prepared[event]); err != nil {
			return nil, err
		}
	}
	if oneShot && prefix.Len() == 0 {
		return nil, fmt.Errorf("native audio sequence is empty")
	}
	if !oneShot && loop.Len() == 0 {
		return nil, fmt.Errorf("native audio loop is empty")
	}
	if oneShot {
		return bytes.NewReader(prefix.Bytes()), nil
	}
	return io.MultiReader(bytes.NewReader(prefix.Bytes()), ebitenaudio.NewInfiniteLoop(bytes.NewReader(loop.Bytes()), int64(loop.Len()))), nil
}

func stereoPCMAtRate(input []byte, format PCMFormat, targetRate int) ([]byte, error) {
	if targetRate <= 0 {
		return nil, fmt.Errorf("invalid target sample rate %d", targetRate)
	}
	pcm, err := ToStereoPCM16(input, format)
	if err != nil {
		return nil, err
	}
	if format.SampleRate != targetRate {
		pcm, err = resampleStereoPCM16(pcm, format.SampleRate, targetRate)
		if err != nil {
			return nil, err
		}
	}
	return pcm, nil
}

func resampleStereoPCM16(input []byte, from, to int) ([]byte, error) {
	if from <= 0 || to <= 0 {
		return nil, fmt.Errorf("invalid resample rate %d to %d", from, to)
	}
	if len(input)%4 != 0 {
		return nil, fmt.Errorf("stereo PCM byte count %d is not frame aligned", len(input))
	}
	if from == to {
		return input, nil
	}
	frames := int64(len(input) / 4)
	if frames != 0 && int64(to) > int64(^uint64(0)>>1)/frames {
		return nil, fmt.Errorf("resampled audio size exceeds the supported range")
	}
	outputFrames := frames * int64(to) / int64(from)
	if outputFrames > int64(int(^uint(0)>>1)/4) {
		return nil, fmt.Errorf("resampled audio size exceeds the supported range")
	}
	source := ebitenaudio.ResampleReader(bytes.NewReader(input), int64(len(input)), from, to)
	return io.ReadAll(io.LimitReader(source, outputFrames*4))
}
