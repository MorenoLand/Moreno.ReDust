package scripts

// integerSquareRoot is FUN_0042E840: the floor of the square root of an
// unsigned 32-bit value, worked out bit by bit as the native routine does. A
// negative number's bit pattern is large, so its root is large too and the
// script's sqrt keeps the low 16 bits.
func integerSquareRoot(value uint32) uint32 {
	var root uint32
	bit := uint32(1) << 30
	for bit > value {
		bit >>= 2
	}
	for bit != 0 {
		if value >= root+bit {
			value -= root + bit
			root = root>>1 + bit
		} else {
			root >>= 1
		}
		bit >>= 2
	}
	return root & 0xffff
}
