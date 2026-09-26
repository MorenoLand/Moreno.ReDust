package scripts

type DispatchRoute uint8

const (
	RouteOther DispatchRoute = iota
	Route00424890
	Route004137B0
	Route004137B0Then00424890
	Route0041D680
	Route0041D6F0
)

func ClassifyDispatch(opcode uint16) DispatchRoute {
	switch {
	case opcode > 11999 && opcode < 0x2f3a:
		return Route00424890
	case opcode > 19999 && opcode < 0x4e8e:
		return Route004137B0
	case opcode > 15999 && opcode < 0x3eb7:
		return Route004137B0Then00424890
	case opcode == 0x0fa2 || opcode == 0x0fa3:
		return Route0041D680
	case opcode == 0x0fbd:
		return Route0041D6F0
	default:
		return RouteOther
	}
}
