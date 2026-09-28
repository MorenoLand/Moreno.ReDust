package audio

import (
	"fmt"
	"sync"

	ebitenaudio "github.com/hajimehoshi/ebiten/v2/audio"
)

type Player struct {
	native  *ebitenaudio.Player
	binding *volumeBinding
}

type volumeBus struct {
	mu       sync.Mutex
	level    int
	bindings map[*volumeBinding]struct{}
}

type volumeBinding struct {
	bus    *volumeBus
	apply  func(float64)
	gain   float64
	closed bool
}

var applicationVolume = &volumeBus{level: 9, bindings: map[*volumeBinding]struct{}{}}

func WaveVolume() int {
	return applicationVolume.getLevel()
}

func SetWaveVolume(level int) error {
	return applicationVolume.setLevel(level)
}

func (b *volumeBus) getLevel() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.level
}

func (b *volumeBus) setLevel(level int) error {
	if level < 0 || level > 9 {
		return fmt.Errorf("wave volume %d is outside 0..9", level)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.level = level
	for binding := range b.bindings {
		binding.applyVolume()
	}
	return nil
}

func (b *volumeBus) bind(apply func(float64), gain float64) *volumeBinding {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bindings == nil {
		b.bindings = map[*volumeBinding]struct{}{}
	}
	binding := &volumeBinding{bus: b, apply: apply, gain: gain}
	b.bindings[binding] = struct{}{}
	binding.applyVolume()
	return binding
}

func newPlayer(native *ebitenaudio.Player) *Player {
	if native == nil {
		return nil
	}
	binding := applicationVolume.bind(native.SetVolume, 1)
	return &Player{native: native, binding: binding}
}

func (b *volumeBinding) applyVolume() {
	if !b.closed && b.apply != nil {
		b.apply(b.gain * float64(b.bus.level) / 9)
	}
}

func (b *volumeBinding) setGain(gain float64) {
	if b == nil || b.bus == nil {
		return
	}
	b.bus.mu.Lock()
	b.gain = gain
	b.applyVolume()
	b.bus.mu.Unlock()
}

func (b *volumeBinding) close() {
	if b == nil || b.bus == nil {
		return
	}
	b.bus.mu.Lock()
	b.closed = true
	delete(b.bus.bindings, b)
	b.bus.mu.Unlock()
}

func (p *Player) SetVolume(gain float64) {
	if p != nil {
		p.binding.setGain(gain)
	}
}

func (p *Player) Play() {
	if p != nil && p.native != nil {
		p.native.Play()
	}
}

func (p *Player) Pause() {
	if p != nil && p.native != nil {
		p.native.Pause()
	}
}

func (p *Player) IsPlaying() bool {
	return p != nil && p.native != nil && p.native.IsPlaying()
}

func (p *Player) Close() error {
	if p == nil || p.native == nil {
		return nil
	}
	p.binding.close()
	return p.native.Close()
}
