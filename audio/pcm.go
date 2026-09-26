package audio

import (
	"encoding/binary"
	"fmt"

	ebitenaudio "github.com/hajimehoshi/ebiten/v2/audio"
)

type PCMFormat struct {
	SampleRate    int
	Channels      int
	BitsPerSample int
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
	return context.NewPlayerFromBytes(pcm), nil
}
