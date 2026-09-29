package scripts

// Complete verified opcode-to-handler tables for the two dispatchers, from live
// Ghidra decompilation of FUN_00424890 and FUN_004137B0 in DF386.EXE (preferred
// base 0x00400000).
//
// Every switch was audited for contiguity, duplicate case labels and
// call-less cases before transcription. All four are contiguous from their base
// with no duplicates and no call-less cases, which is the check that would have
// caught the earlier extraction bug: a case pattern matching only "case 0xN:"
// silently dropped cases 0 through 10 of every switch, because Ghidra writes
// "case 0:" for single-digit cases. See the ledger for the correction.
//
// These tables are handler identity only. They record which native function the
// dispatcher calls for an opcode; they do not imply the body is translated.

// commandPrimaryHandlers is the complete verified FUN_00424890 primary switch:
// opcode 12001..12088, contiguous, 88 cases, no gaps.
var commandPrimaryHandlers = map[uint16]string{
	12001: "FUN_00425B70",
	12002: "FUN_00425D10",
	12003: "FUN_00426350",
	12004: "FUN_004264E0",
	12005: "FUN_0040F9B0",
	12006: "FUN_0040FF40",
	12007: "FUN_00410760",
	12008: "FUN_004279A0",
	12009: "FUN_00425D30",
	12010: "FUN_004265D0",
	12011: "FUN_00426630",
	12012: "FUN_004266B0",
	12013: "FUN_0040BFA0",
	12014: "FUN_0040C4C0",
	12015: "FUN_0040C900",
	12016: "FUN_0040CB40",
	12017: "FUN_00426880",
	12018: "FUN_00407D90",
	12019: "FUN_0040E250",
	12020: "FUN_0040E540",
	12021: "FUN_0040E6F0",
	12022: "FUN_0040E770",
	12023: "FUN_0040E7B0",
	12024: "FUN_0040E7F0",
	12025: "FUN_0040E830",
	12026: "FUN_0040E870",
	12027: "FUN_0041DEF0",
	12028: "FUN_0040E8D0",
	12029: "FUN_0040E8B0",
	12030: "FUN_0040E8F0",
	12031: "FUN_004182F0",
	12032: "FUN_00419520",
	12033: "FUN_00419880",
	12034: "FUN_0041A180",
	12035: "FUN_0041AA40",
	12036: "FUN_0041AAB0",
	12037: "FUN_0041AB70",
	12038: "FUN_00425E30",
	12039: "FUN_00426230",
	12040: "FUN_00426370",
	12041: "FUN_004086D0",
	12042: "FUN_00408120",
	12043: "FUN_004085E0",
	12044: "FUN_00408430",
	12045: "FUN_00408930",
	12046: "FUN_00408AD0",
	12047: "FUN_0040C9B0",
	12048: "FUN_0040CEA0",
	12049: "FUN_00426410",
	12050: "FUN_00426390",
	12051: "FUN_00426490",
	12052: "FUN_00426200",
	12053: "FUN_00426380",
	12054: "FUN_00420A80",
	12055: "FUN_00420CC0",
	12056: "FUN_00420110",
	12057: "FUN_00420630",
	12058: "FUN_00420B30",
	12059: "FUN_00421020",
	12060: "FUN_00411860",
	12061: "FUN_004119C0",
	12062: "FUN_00411AD0",
	12063: "FUN_00412840",
	12064: "FUN_004128B0",
	12065: "FUN_00412970",
	12066: "FUN_0041A5A0",
	12067: "FUN_0041A920",
	12068: "FUN_00412A70",
	12069: "FUN_00412EC0",
	12070: "FUN_00413210",
	12071: "FUN_00426580",
	12072: "FUN_0040FE00",
	12073: "FUN_004265B0",
	12074: "FUN_00408700",
	12075: "FUN_0040A860",
	12076: "FUN_0041E840",
	12077: "FUN_00422C80",
	12078: "FUN_00422D40",
	12079: "FUN_00425B10",
	12080: "FUN_00425A00",
	12081: "FUN_00418400",
	12082: "FUN_00425E90",
	12083: "FUN_00408FD0",
	12084: "FUN_00408750",
	12085: "FUN_0040B450",
	12086: "FUN_0041F7E0",
	12087: "FUN_0041F860",
	12088: "FUN_0040B4D0",
}

// commandSecondaryHandlers is the complete verified FUN_00424890 secondary switch:
// opcode 16002..16053, contiguous, 52 cases, no gaps.
var commandSecondaryHandlers = map[uint16]string{
	16002: "FUN_0040AF20",
	16003: "FUN_0040B6B0",
	16004: "FUN_0040BA60",
	16005: "FUN_0040BE60",
	16006: "FUN_0040ABD0",
	16007: "FUN_0041AC70",
	16008: "FUN_00412450",
	16009: "FUN_00425BD0",
	16010: "FUN_00425CD0",
	16011: "FUN_0041ACE0",
	16012: "FUN_0040AFC0",
	16013: "FUN_0041F350",
	16014: "FUN_0040ADA0",
	16015: "FUN_0041F910",
	16016: "FUN_0041F2B0",
	16017: "FUN_0041FBD0",
	16018: "FUN_0041FDF0",
	16019: "FUN_0041FFD0",
	16020: "FUN_0041EF60",
	16021: "FUN_0040AA00",
	16022: "FUN_00426190",
	16023: "FUN_0040B1C0",
	16024: "FUN_0040B280",
	16025: "FUN_0041F130",
	16026: "FUN_0041F550",
	16027: "FUN_0041E9E0",
	16028: "FUN_0041F610",
	16029: "FUN_004199A0",
	16030: "FUN_00426900",
	16031: "FUN_0041AE60",
	16032: "FUN_0041EB30",
	16033: "FUN_00425C60",
	16034: "FUN_0040BC40",
	16035: "FUN_00425AC0",
	16036: "FUN_0040D240",
	16037: "FUN_004259A0",
	16038: "FUN_0040EAF0",
	16039: "FUN_0040EBC0",
	16040: "FUN_0040E910",
	16041: "FUN_0040E9E0",
	16042: "FUN_0041EC00",
	16043: "FUN_0040D380",
	16044: "FUN_0040D450",
	16045: "FUN_00426530",
	16046: "FUN_00426710",
	16047: "FUN_00426810",
	16048: "FUN_004267A0",
	16049: "FUN_00408DD0",
	16050: "FUN_00408D30",
	16051: "FUN_0040D4F0",
	16052: "FUN_0041ECA0",
	16053: "FUN_004087B0",
}

// valuePrimaryHandlers is the complete verified FUN_004137B0 first switch:
// opcode 16001..16053, contiguous, 53 cases, no gaps.
var valuePrimaryHandlers = map[uint16]string{
	16001: "FUN_0040B620",
	16002: "FUN_0040B130",
	16003: "FUN_0040B7B0",
	16004: "FUN_0040BB50",
	16005: "FUN_0040BF10",
	16006: "FUN_0040AD20",
	16007: "FUN_0041AF90",
	16008: "FUN_00412410",
	16009: "FUN_00415B70",
	16010: "FUN_00415C20",
	16011: "FUN_0041B070",
	16012: "FUN_0040B070",
	16013: "FUN_0041F400",
	16014: "FUN_0040AEA0",
	16015: "FUN_0041F9B0",
	16016: "FUN_0041F4C0",
	16017: "FUN_0041FCD0",
	16018: "FUN_0041FEE0",
	16019: "FUN_00420080",
	16020: "FUN_0041F0B0",
	16021: "FUN_0040AB50",
	16022: "FUN_00416540",
	16023: "FUN_0040B3C0",
	16024: "FUN_0040B330",
	16025: "FUN_0041F230",
	16026: "FUN_0041F750",
	16027: "FUN_0041ED40",
	16028: "FUN_0041F6C0",
	16029: "FUN_0041B3E0",
	16030: "FUN_00416570",
	16031: "FUN_0041B2B0",
	16032: "FUN_0041EDC0",
	16033: "FUN_00415BF0",
	16034: "FUN_0040BD50",
	16035: "FUN_004156F0",
	16036: "FUN_0040D2F0",
	16037: "FUN_00414F20",
	16038: "FUN_0040EDC0",
	16039: "FUN_0040EE20",
	16040: "FUN_0040ED60",
	16041: "FUN_0040ECC0",
	16042: "FUN_0041EE40",
	16043: "FUN_0040D590",
	16044: "FUN_0040D610",
	16045: "FUN_00414F50",
	16046: "FUN_00416140",
	16047: "FUN_00416240",
	16048: "FUN_004161D0",
	16049: "FUN_00408EF0",
	16050: "FUN_00408D90",
	16051: "FUN_0040D6A0",
	16052: "FUN_0041EED0",
	16053: "FUN_00408830",
}

// valueSecondaryHandlers is the complete verified FUN_004137B0 second switch:
// opcode 20002..20108, contiguous, 107 cases, no gaps.
var valueSecondaryHandlers = map[uint16]string{
	20002: "FUN_00415CB0",
	20003: "FUN_00415D10",
	20004: "FUN_00415D70",
	20005: "FUN_00415DF0",
	20006: "FUN_00415E20",
	20007: "FUN_00415E90",
	20008: "FUN_00415E60",
	20009: "FUN_004162B0",
	20010: "FUN_00416320",
	20011: "FUN_004163B0",
	20012: "FUN_0040D030",
	20013: "FUN_0040D090",
	20014: "FUN_0040EEE0",
	20015: "FUN_0040EF60",
	20016: "FUN_0040EE80",
	20017: "FUN_0041B450",
	20018: "FUN_0041B4A0",
	20019: "FUN_0041B4F0",
	20020: "FUN_0041B540",
	20021: "FUN_0041B600",
	20022: "FUN_0041B6B0",
	20023: "FUN_0041AFD0",
	20024: "FUN_0041B020",
	20025: "FUN_00416420",
	20026: "FUN_004164A0",
	20027: "FUN_00416510",
	20028: "FUN_00408520",
	20029: "FUN_0040D060",
	20030: "FUN_0040D190",
	20031: "FUN_004211B0",
	20032: "FUN_00421210",
	20033: "FUN_004211E0",
	20034: "FUN_00421310",
	20035: "FUN_00411FC0",
	20036: "FUN_00412010",
	20037: "FUN_00412390",
	20038: "FUN_004125C0",
	20039: "FUN_0040B8D0",
	20040: "FUN_0041FA40",
	20041: "FUN_0041B330",
	20042: "FUN_00412690",
	20043: "FUN_00412740",
	20044: "FUN_00412110",
	20045: "FUN_00412210",
	20046: "FUN_00408B90",
	20047: "FUN_00408C00",
	20048: "FUN_00412650",
	20049: "FUN_00408CF0",
	20050: "FUN_00415AC0",
	20051: "FUN_00415A80",
	20052: "FUN_004159E0",
	20053: "FUN_0041B2F0",
	20054: "FUN_00416650",
	20055: "FUN_004167E0",
	20056: "FUN_004168C0",
	20057: "FUN_00416930",
	20058: "FUN_00416BC0",
	20059: "FUN_00416BF0",
	20060: "FUN_00416C20",
	20061: "FUN_00416C50",
	20062: "FUN_00416CD0",
	20063: "FUN_00416D50",
	20064: "FUN_00416E30",
	20065: "FUN_00415720",
	20066: "FUN_00415750",
	20067: "FUN_00415780",
	20068: "FUN_00415970",
	20069: "FUN_004158D0",
	20070: "FUN_004153E0",
	20071: "FUN_004151E0",
	20072: "FUN_00415330",
	20073: "FUN_00414FB0",
	20074: "FUN_00414F80",
	20075: "FUN_0040F010",
	20076: "FUN_0040F040",
	20077: "FUN_0040F0F0",
	20078: "FUN_0040F1D0",
	20079: "FUN_0040F130",
	20080: "FUN_0040EC70",
	20081: "FUN_00415290",
	20082: "FUN_00416F10",
	20083: "FUN_00415840",
	20084: "FUN_0040CB60",
	20085: "FUN_0041A1A0",
	20086: "FUN_00408950",
	20087: "FUN_0040CEC0",
	20088: "FUN_00420CE0",
	20089: "FUN_00421040",
	20090: "FUN_0041A5C0",
	20091: "FUN_0041A940",
	20092: "FUN_00412A90",
	20093: "FUN_00412EE0",
	20094: "FUN_00413230",
	20095: "FUN_00418420",
	20096: "FUN_004150C0",
	20097: "FUN_0040EEB0",
	20098: "FUN_0041DF30",
	20099: "FUN_00414EB0",
	20100: "FUN_0041B700",
	20101: "FUN_00416040",
	20102: "FUN_00415F90",
	20103: "FUN_004160C0",
	20104: "FUN_00415F60",
	20105: "FUN_00415EC0",
	20106: "FUN_00415F30",
	20107: "FUN_004090B0",
	20108: "FUN_00414E40",
}

// The four tables are transcribed from the reference's **jump tables**, not from
// decompiled C. That is a materially stronger kind of evidence, and it was used to
// validate the transcription: every entry of every table was read out of the image,
// the block each entry points at was scanned for its CALL, and the target compared
// with the value transcribed here. **All 300 cases match, with zero mismatches.**
//
// The dispatchers are not written as switches. Both use the same shape:
//
//	MOVSX EAX, word [record]        ; the opcode
//	CMP  EAX, special
//	JZ   <the special case>
//	SUB  EAX, bandBase
//	CMP  EAX, bandCount - 1
//	JA   <the default, which returns status 1>
//	JMP  dword [EAX * 4 + table]
//
// The two dispatchers **mirror each other's special case**. FUN_00424890 tests
// opcode 16001 before consulting either of its tables, so 16001 has no command-side
// entry at all and is handled entirely out of switch. FUN_004137B0 tests opcode
// 20001 the same way, and 20001 likewise has no value-side entry; its first table
// entry is 16001 at index 0, which is why 16001 is dispatched on the value side and
// not on the command side.
const (
	// CommandSpecialOpcode is 16001, which FUN_00424890 handles before either of
	// its tables, at 0x004251E0. It is the only opcode with no command-side entry
	// inside either band.
	CommandSpecialOpcode uint16 = 16001
	// ValueSpecialOpcode is 20001, which FUN_004137B0 handles before either of its
	// tables, at 0x00413E7B. This is the value dispatcher's mirror of
	// CommandSpecialOpcode, and it is the only opcode with no value-side entry
	// inside either band.
	ValueSpecialOpcode uint16 = 20001
	// UndispatchedStatus is the status both dispatchers return from their default
	// arm, for an opcode outside every band.
	UndispatchedStatus uint16 = 1
	// FrameStatus is the status both return when the record after the opcode is
	// not the "(" token, checked before any table is consulted.
	FrameStatus uint16 = 2
)

// The jump table geometry, verified from the image. Each entry is four bytes and
// holds the address of the block for one opcode.
var (
	// commandPrimaryTable is at 0x00425768, indexed by opcode - 0x2EE1.
	commandPrimaryTable = TableGeometry{Address: 0x00425768, Base: 0x2EE1, Count: 0x58}
	// commandSecondaryTable is at 0x004258C8, indexed by opcode - 0x3E82.
	commandSecondaryTable = TableGeometry{Address: 0x004258C8, Base: 0x3E82, Count: 0x34}
	// valuePrimaryTable is at 0x00414BB8, indexed by opcode - 0x3E81.
	valuePrimaryTable = TableGeometry{Address: 0x00414BB8, Base: 0x3E81, Count: 0x35}
	// valueSecondaryTable is at 0x00414C8C, indexed by opcode - 0x4E22.
	valueSecondaryTable = TableGeometry{Address: 0x00414C8C, Base: 0x4E22, Count: 0x6B}
)

// TableGeometry describes one jump table in the reference image.
type TableGeometry struct {
	// Address is where the table sits.
	Address uint32
	// Base is the opcode the first entry handles; index i handles Base + i.
	Base uint16
	// Count is the number of entries, so the table covers Base..Base+Count-1.
	Count uint16
}

// First and Last are the opcodes the table covers.
func (g TableGeometry) First() uint16 { return g.Base }
func (g TableGeometry) Last() uint16  { return g.Base + g.Count - 1 }

// Covers reports whether an opcode is a table entry rather than a special case or
// a default. This is the authoritative test, replacing any range arithmetic: the
// bands are not contiguous in the way the bases suggest, because 16001 and 20001
// are handled out of table.
func (g TableGeometry) Covers(opcode uint16) bool {
	return opcode >= g.First() && opcode <= g.Last()
}

// commandGeometry and valueGeometry describe the two dispatchers in full, so a
// caller can classify an opcode without consulting the handler tables.
var (
	commandGeometry = []TableGeometry{commandPrimaryTable, commandSecondaryTable}
	valueGeometry   = []TableGeometry{valuePrimaryTable, valueSecondaryTable}
)

// DispatchRouteFromGeometry classifies an opcode the way the dispatcher does:
// the special case first, then each table in order, then the default. It is a
// second, independent route to the same answer as the handler tables, and a test
// asserts the two never disagree.
func DispatchRouteFromGeometry(opcode uint16, special uint16, geometry []TableGeometry) TableGeometry {
	if opcode == special {
		return TableGeometry{}
	}
	for _, table := range geometry {
		if table.Covers(opcode) {
			return table
		}
	}
	return TableGeometry{}
}

// verifiedTables describes every transcribed switch so a test can assert the
// exact case count, endpoints and contiguity. A dropped case changes the count
// and fails the assertion, which is the guard against repeating the earlier
// extraction error.
//
// The geometry each table was transcribed from is carried alongside it, so a test
// can confirm the entry counts against the jump tables rather than only against
// the Go maps.
var verifiedTables = []struct {
	name     string
	table    map[uint16]string
	first    uint16
	last     uint16
	geometry TableGeometry
}{
	{"FUN_00424890.primary", commandPrimaryHandlers, 12001, 12088, commandPrimaryTable},
	{"FUN_00424890.secondary", commandSecondaryHandlers, 16002, 16053, commandSecondaryTable},
	{"FUN_004137B0.primary", valuePrimaryHandlers, 16001, 16053, valuePrimaryTable},
	{"FUN_004137B0.secondary", valueSecondaryHandlers, 20002, 20108, valueSecondaryTable},
}

// normalizeHandlerName renders a native function name in the canonical
// uppercase-hex form used throughout the ledger, so a transcription taken from
// Ghidra's lowercase output compares equal to a hand-written reference.
func normalizeHandlerName(name string) string {
	if len(name) != len("FUN_")+8 {
		return name
	}
	prefix, digits := name[:4], name[4:]
	out := []byte(prefix)
	for i := 0; i < len(digits); i++ {
		c := digits[i]
		if c >= 'a' && c <= 'f' {
			c -= 'a' - 'A'
		}
		out = append(out, c)
	}
	return string(out)
}

// CommandHandlerFor returns the verified native handler for opcode on the given
// side of the hybrid band. The value dispatcher is consulted first for the
// 16000 band, matching verified FUN_004192D0, and the command dispatcher
// otherwise. preferred selects which side to consult for the 16000 band.
// Returned names are normalized to uppercase hex.
func CommandHandlerFor(opcode uint16, preferred HybridPreference) (handler string, found bool) {
	lookup := func() (string, bool) {
		if h, ok := valuePrimaryHandlers[opcode]; ok {
			return normalizeHandlerName(h), true
		}
		if h, ok := valueSecondaryHandlers[opcode]; ok {
			return normalizeHandlerName(h), true
		}
		return "", false
	}
	command := func() (string, bool) {
		if h, ok := commandPrimaryHandlers[opcode]; ok {
			return normalizeHandlerName(h), true
		}
		if h, ok := commandSecondaryHandlers[opcode]; ok {
			return normalizeHandlerName(h), true
		}
		return "", false
	}
	if preferred == PreferValue {
		if h, ok := lookup(); ok {
			return h, true
		}
		if h, ok := command(); ok {
			return h, true
		}
		return "", false
	}
	if h, ok := command(); ok {
		return h, true
	}
	if h, ok := lookup(); ok {
		return h, true
	}
	return "", false
}

// HybridPreference selects which dispatcher is consulted first for the 16000
// band. Verified FUN_004192D0 tries the value dispatcher first and only falls
// back to the command dispatcher when it returns a non-zero status.
type HybridPreference uint8

const (
	// PreferValue matches the native first attempt.
	PreferValue HybridPreference = iota
	// PreferCommand inverts the order, which is useful for diagnostics that need
	// the command-side handler for a hybrid opcode.
	PreferCommand
)
