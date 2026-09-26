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

func NewPlayer(context *ebitenaudio.Context, input []byte, format PCMFormat) (*ebitenaudio.Player, error) {
	if context == nil {
		return nil, fmt.Errorf("audio context is nil")
	}
	pcm, err := ToStereoPCM16(input, format)
	if err != nil {
		return nil, err
	}
	if format.SampleRate != context.SampleRate() {
		pcm, err = resampleStereoPCM16(pcm, format.SampleRate, context.SampleRate())
		if err != nil {
			return nil, err
		}
	}
	return context.NewPlayerFromBytes(pcm), nil
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
