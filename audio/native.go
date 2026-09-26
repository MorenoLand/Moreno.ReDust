package audio

import (
	"encoding/binary"
	"fmt"
	"strings"

	ebitenaudio "github.com/hajimehoshi/ebiten/v2/audio"

	"redust/assets"
)

const nativeSoundTableOffset = 0xb2
const nativeSoundRowSize = 0x18
const nativeSoundHeaderSize = 0x28

type nativeSoundEntry struct {
	resource uint32
	name     []byte
}

type NativeSound struct {
	Metadata NativeSoundMetadata
	Samples  []byte
	Format   PCMFormat
}

type SoundBank struct {
	resources *assets.ResourceCache
	sounds    map[string]nativeSoundEntry
	players   []*ebitenaudio.Player
}

var nativeSoundDelta = [16]int8{0, 1, 2, 3, 4, 5, 6, 7, -8, -7, -6, -5, -4, -3, -2, -1}

func OpenSoundBank(workspace assets.Workspace, name string) (*SoundBank, error) {
	resources, err := workspace.OpenResourceCache(name)
	if err != nil {
		return nil, err
	}
	failed := true
	defer func() {
		if failed {
			resources.Close()
		}
	}()
	header, err := resources.Header()
	if err != nil {
		return nil, err
	}
	lease, err := resources.Acquire(0)
	if err != nil {
		return nil, err
	}
	metadata, err := lease.Bytes()
	if closeErr := lease.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	if len(metadata) < nativeSoundTableOffset {
		return nil, fmt.Errorf("sound bank metadata is shorter than its table header")
	}
	entryCount := int(binary.LittleEndian.Uint16(metadata[0x18:0x1a])) + int(binary.LittleEndian.Uint16(metadata[0x1a:0x1c]))
	if entryCount > (len(metadata)-nativeSoundTableOffset)/nativeSoundRowSize || uint64(entryCount)+1 > uint64(header.CountB) {
		return nil, fmt.Errorf("sound bank has %d sample entries outside its metadata or APPL table", entryCount)
	}
	sounds := make(map[string]nativeSoundEntry, entryCount)
	for index := 0; index < entryCount; index++ {
		row := nativeSoundTableOffset + index*nativeSoundRowSize
		nameStart := row + 8
		nameLength := int(metadata[nameStart])
		if nameLength+1 > row+nativeSoundRowSize-nameStart {
			return nil, fmt.Errorf("sound bank row %d has a truncated Pascal name", index)
		}
		pascalName := append([]byte(nil), metadata[nameStart:nameStart+nameLength+1]...)
		key := strings.ToLower(string(pascalName[1:]))
		if key != "" {
			if _, exists := sounds[key]; !exists {
				sounds[key] = nativeSoundEntry{resource: uint32(index + 1), name: pascalName}
			}
		}
	}
	failed = false
	return &SoundBank{resources: resources, sounds: sounds}, nil
}

func (b *SoundBank) Names() []string {
	if b == nil {
		return nil
	}
	names := make([]string, 0, len(b.sounds))
	for name := range b.sounds {
		names = append(names, name)
	}
	return names
}

func (b *SoundBank) Load(name string) (NativeSound, error) {
	if b == nil || b.resources == nil {
		return NativeSound{}, fmt.Errorf("sound bank is closed")
	}
	entry, ok := b.sounds[strings.ToLower(name)]
	if !ok {
		return NativeSound{}, fmt.Errorf("sound %q is not in the bank", name)
	}
	lease, err := b.resources.Acquire(entry.resource)
	if err != nil {
		return NativeSound{}, err
	}
	resource, err := lease.Bytes()
	if closeErr := lease.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return NativeSound{}, err
	}
	metadata, err := ParseNativeSoundMetadata(resource, entry.name)
	if err != nil {
		return NativeSound{}, err
	}
	if metadata.Channels != 1 {
		return NativeSound{}, fmt.Errorf("native sound %q has unsupported channel count %d", name, metadata.Channels)
	}
	if metadata.Field24PerChannel <= 0 {
		return NativeSound{}, fmt.Errorf("native sound %q has invalid sample count %d", name, metadata.Field24PerChannel)
	}
	samples, err := DecodeNativeSound(resource, int(metadata.Field24PerChannel))
	if err != nil {
		return NativeSound{}, fmt.Errorf("decode native sound %q: %w", name, err)
	}
	return NativeSound{Metadata: metadata, Samples: samples, Format: PCMFormat{SampleRate: int(metadata.SampleRate), Channels: 1, BitsPerSample: 8}}, nil
}

func (b *SoundBank) Play(context *ebitenaudio.Context, name string, gain float64) error {
	if context == nil {
		return fmt.Errorf("audio context is nil")
	}
	if gain <= 0 {
		return fmt.Errorf("sound gain %g is invalid", gain)
	}
	if b == nil || b.resources == nil {
		return fmt.Errorf("sound bank is closed")
	}
	active := b.players[:0]
	for _, player := range b.players {
		if player.IsPlaying() {
			active = append(active, player)
		} else if err := player.Close(); err != nil {
			return err
		}
	}
	b.players = active
	sound, err := b.Load(name)
	if err != nil {
		return err
	}
	for i, sample := range sound.Samples {
		value := int(float64(int(sample)-128) * gain)
		if value > 127 {
			value = 127
		} else if value < -128 {
			value = -128
		}
		sound.Samples[i] = byte(value + 128)
	}
	player, err := NewPlayer(context, sound.Samples, sound.Format)
	if err != nil {
		return err
	}
	player.SetVolume(1)
	player.Play()
	b.players = append(b.players, player)
	return nil
}

func (b *SoundBank) ActivePlayers() int {
	if b == nil {
		return 0
	}
	active := 0
	for _, player := range b.players {
		if player.IsPlaying() {
			active++
		}
	}
	return active
}

func DecodeNativeSoundResource(resource []byte) (NativeSound, error) {
	metadata, err := ParseNativeSoundMetadata(resource, []byte{0})
	if err != nil {
		return NativeSound{}, err
	}
	if metadata.Channels != 1 {
		return NativeSound{}, fmt.Errorf("native sound resource has unsupported channel count %d", metadata.Channels)
	}
	if metadata.Field24PerChannel <= 0 {
		return NativeSound{}, fmt.Errorf("native sound resource has invalid sample count %d", metadata.Field24PerChannel)
	}
	samples, err := DecodeNativeSound(resource, int(metadata.Field24PerChannel))
	if err != nil {
		return NativeSound{}, err
	}
	return NativeSound{Metadata: metadata, Samples: samples, Format: PCMFormat{SampleRate: int(metadata.SampleRate), Channels: 1, BitsPerSample: 8}}, nil
}

func DecodeNativeSound(resource []byte, sampleCount int) ([]byte, error) {
	if sampleCount <= 0 {
		return nil, fmt.Errorf("native sound sample count %d is invalid", sampleCount)
	}
	if len(resource) < nativeSoundHeaderSize+8 {
		return nil, fmt.Errorf("native sound resource has no complete block table")
	}
	blockCount := int(binary.LittleEndian.Uint32(resource[nativeSoundHeaderSize : nativeSoundHeaderSize+4]))
	if blockCount < 1 || blockCount+1 > (len(resource)-nativeSoundHeaderSize-4)/4 {
		return nil, fmt.Errorf("native sound block count %d exceeds its offset table", blockCount)
	}
	if sampleCount%blockCount != 0 {
		return nil, fmt.Errorf("native sound sample count %d is not divisible by %d blocks", sampleCount, blockCount)
	}
	tableStart := nativeSoundHeaderSize + 4
	tableEnd := tableStart + (blockCount+1)*4
	samples := make([]byte, 0, sampleCount)
	for block := 0; block < blockCount; block++ {
		start := int(binary.LittleEndian.Uint32(resource[tableStart+block*4 : tableStart+block*4+4]))
		end := int(binary.LittleEndian.Uint32(resource[tableStart+(block+1)*4 : tableStart+(block+1)*4+4]))
		if start < tableEnd || end < start || end > len(resource) {
			return nil, fmt.Errorf("native sound block %d has invalid byte range [%d,%d)", block, start, end)
		}
		decoded, err := decodeNativeSoundBlock(resource[start:end], sampleCount/blockCount)
		if err != nil {
			return nil, fmt.Errorf("native sound block %d: %w", block, err)
		}
		samples = append(samples, decoded...)
	}
	return samples, nil
}

func decodeNativeSoundBlock(encoded []byte, sampleCount int) ([]byte, error) {
	if sampleCount <= 0 || len(encoded) == 0 {
		return nil, fmt.Errorf("encoded block or output sample count is empty")
	}
	samples := make([]byte, sampleCount)
	predictor, input, output := uint16(encoded[0]), 1, 0
	samples[output] = nativeSampleByte(predictor)
	output++
	for output < len(samples) {
		if input >= len(encoded) {
			return nil, fmt.Errorf("sample stream ended after %d of %d output samples", output, len(samples))
		}
		command := encoded[input]
		input++
		switch {
		case command&0x80 == 0:
			samples[output] = byte(int(command) + 0x40)
			predictor = uint16(command)
			output++
		case command&0x40 == 0:
			count := int(command&0x3f) + 1
			if count*2 > len(samples)-output {
				return nil, fmt.Errorf("delta run at byte %d exceeds the %d-sample output", input-1, len(samples))
			}
			for i := 0; i < count; i++ {
				if input >= len(encoded) {
					return nil, fmt.Errorf("delta run at byte %d is truncated", input-1)
				}
				delta := encoded[input]
				input++
				high, low := nativeSoundDelta[delta>>4], nativeSoundDelta[delta&0x0f]
				predictor = uint16(int32(predictor) + int32(high))
				samples[output] = nativeSampleByte(predictor)
				output++
				predictor = uint16(int32(predictor) + int32(high) + int32(low))
				samples[output] = nativeSampleByte(predictor)
				output++
			}
		default:
			count := int(command&0x3f) + 1
			if count > len(samples)-output {
				return nil, fmt.Errorf("repeat run at byte %d exceeds the %d-sample output", input-1, len(samples))
			}
			sample := nativeSampleByte(predictor)
			for i := 0; i < count; i++ {
				samples[output] = sample
				output++
			}
		}
	}
	return samples, nil
}

func nativeSampleByte(predictor uint16) byte {
	return byte(int(int8(byte(predictor))) + 0x40)
}

func (b *SoundBank) Close() error {
	if b == nil || b.resources == nil {
		return nil
	}
	var closeErr error
	for _, player := range b.players {
		if err := player.Close(); closeErr == nil && err != nil {
			closeErr = err
		}
	}
	b.players = nil
	err := b.resources.Close()
	b.resources = nil
	if closeErr != nil {
		return closeErr
	}
	return err
}
