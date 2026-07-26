package steamauth

import (
	"encoding/binary"
	"errors"
	"math"
)

// A minimal protobuf encoder and decoder.
//
// Steam's IAuthenticationService speaks protobuf over HTTP. Six small messages are needed —
// three requests and three responses — and none of them contains a nested repeated group
// deeper than one level. Hand-encoding those is a few hundred lines with no code generation
// step, no .proto files to keep in sync with Valve, and no toolchain dependency added to a
// desktop app that otherwise has none.
//
// The trade is that field numbers live here as constants instead of in a schema. They are
// stable published wire format, and every one is named at its use site.

const (
	wireVarint  = 0
	wireFixed64 = 1
	wireBytes   = 2
	wireFixed32 = 5
)

var errMalformed = errors.New("malformed protobuf")

// --- encoding ---------------------------------------------------------------------------

type protoBuf struct{ b []byte }

func (p *protoBuf) tag(field, wire int) {
	p.varint(uint64(field)<<3 | uint64(wire))
}

func (p *protoBuf) varint(v uint64) {
	for v >= 0x80 {
		p.b = append(p.b, byte(v)|0x80)
		v >>= 7
	}
	p.b = append(p.b, byte(v))
}

// PutUint64 writes an unsigned integer field. Zero values are skipped: proto3 does not put
// defaults on the wire, and sending them makes the request differ from what Steam's own
// clients send.
func (p *protoBuf) PutUint64(field int, v uint64) {
	if v == 0 {
		return
	}
	p.tag(field, wireVarint)
	p.varint(v)
}

func (p *protoBuf) PutInt32(field int, v int32) {
	if v == 0 {
		return
	}
	p.tag(field, wireVarint)
	p.varint(uint64(v))
}

func (p *protoBuf) PutBool(field int, v bool) {
	if !v {
		return
	}
	p.tag(field, wireVarint)
	p.varint(1)
}

// PutFixed64 writes a fixed64 field. Steam declares `steamid` as fixed64 in the auth
// messages while declaring `client_id` as a plain uint64 varint, so the two cannot share an
// encoder even though both are 64-bit unsigned.
func (p *protoBuf) PutFixed64(field int, v uint64) {
	if v == 0 {
		return
	}
	p.tag(field, wireFixed64)
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	p.b = append(p.b, b[:]...)
}

func (p *protoBuf) PutString(field int, s string) {
	if s == "" {
		return
	}
	p.PutBytes(field, []byte(s))
}

func (p *protoBuf) PutBytes(field int, b []byte) {
	if len(b) == 0 {
		return
	}
	p.tag(field, wireBytes)
	p.varint(uint64(len(b)))
	p.b = append(p.b, b...)
}

// PutMessage writes a nested message built by fn.
func (p *protoBuf) PutMessage(field int, fn func(*protoBuf)) {
	var inner protoBuf
	fn(&inner)
	if len(inner.b) == 0 {
		return
	}
	p.PutBytes(field, inner.b)
}

func (p *protoBuf) Bytes() []byte { return p.b }

// --- decoding ---------------------------------------------------------------------------

// field is one decoded wire field. Only the representation matching its wire type is
// populated; the accessors below pick the right one.
type field struct {
	num   int
	wire  int
	num64 uint64
	data  []byte
}

// message is a decoded protobuf, keyed by field number. Repeated fields keep every
// occurrence, in order.
type message map[int][]field

func decode(b []byte) (message, error) {
	out := message{}
	for len(b) > 0 {
		key, n := binary.Uvarint(b)
		if n <= 0 {
			return nil, errMalformed
		}
		b = b[n:]
		f := field{num: int(key >> 3), wire: int(key & 7)}
		switch f.wire {
		case wireVarint:
			v, n := binary.Uvarint(b)
			if n <= 0 {
				return nil, errMalformed
			}
			f.num64, b = v, b[n:]
		case wireFixed64:
			if len(b) < 8 {
				return nil, errMalformed
			}
			f.num64, b = binary.LittleEndian.Uint64(b), b[8:]
		case wireBytes:
			l, n := binary.Uvarint(b)
			if n <= 0 {
				return nil, errMalformed
			}
			b = b[n:]
			if uint64(len(b)) < l {
				return nil, errMalformed
			}
			f.data, b = b[:l], b[l:]
		case wireFixed32:
			if len(b) < 4 {
				return nil, errMalformed
			}
			f.num64, b = uint64(binary.LittleEndian.Uint32(b)), b[4:]
		default:
			// Groups (wire types 3 and 4) are not used by these messages and cannot be
			// skipped without knowing the schema, so an unknown type is fatal rather than
			// silently truncating the rest of the message.
			return nil, errMalformed
		}
		out[f.num] = append(out[f.num], f)
	}
	return out, nil
}

func (m message) uint64(num int) uint64 {
	if f, ok := m.last(num); ok {
		return f.num64
	}
	return 0
}

func (m message) bool(num int) bool { return m.uint64(num) != 0 }

func (m message) string(num int) string {
	if f, ok := m.last(num); ok {
		return string(f.data)
	}
	return ""
}

func (m message) bytes(num int) []byte {
	if f, ok := m.last(num); ok {
		return f.data
	}
	return nil
}

// float32 reads a fixed32 field as an IEEE-754 float. Steam reports the poll interval this
// way.
func (m message) float32(num int) float32 {
	if f, ok := m.last(num); ok && f.wire == wireFixed32 {
		return math.Float32frombits(uint32(f.num64))
	}
	return 0
}

// repeated returns every occurrence of a length-delimited field, decoded as nested
// messages. Used for allowed_confirmations, which is how Steam says which Guard method an
// account needs.
func (m message) repeated(num int) []message {
	var out []message
	for _, f := range m[num] {
		if f.wire != wireBytes {
			continue
		}
		sub, err := decode(f.data)
		if err != nil {
			continue
		}
		out = append(out, sub)
	}
	return out
}

// last returns the final occurrence of a field. proto3 says the last value wins for
// non-repeated fields.
func (m message) last(num int) (field, bool) {
	fs := m[num]
	if len(fs) == 0 {
		return field{}, false
	}
	return fs[len(fs)-1], true
}
