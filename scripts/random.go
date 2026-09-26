package scripts

type NativeRandom struct {
	index int
	state [55]uint32
}

func NativeRandomSeed(tick uint32) uint32 {
	return NativeFrameUnits(tick)
}

func NativeFrameUnits(tick uint32) uint32 { return tick * 3 / 50 }

func NewNativeRandom(seed uint32) NativeRandom {
	random := NativeRandom{}
	random.state[0] = seed
	for i := 1; i < len(random.state); i++ {
		random.state[i] = (random.state[i-1]*31 + 1) & 0x7fffffff
	}
	return random
}

func (r *NativeRandom) Inclusive(max uint32) uint32 {
	r.index = (r.index + 1) % len(r.state)
	left, right := (r.index+len(r.state)-1)%len(r.state), (r.index+23)%len(r.state)
	sum := r.state[left] + r.state[right]
	r.state[r.index] = sum & 0x7fffffff
	value := sum & 0x7fff
	if value == 0x7fff {
		value = 0x7ffe
	}
	return uint32(uint64(value)*uint64(max)/0x7fff + 1)
}
