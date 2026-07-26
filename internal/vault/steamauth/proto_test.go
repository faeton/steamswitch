package steamauth

import (
	"bytes"
	"encoding/hex"
	"math"
	"testing"
)

// The encoder is hand-written against a published wire format, so it is pinned against
// bytes computed from the specification rather than against itself. A drift here shows up
// live as "Steam rejected the request" with nothing to point at.
func TestEncoder_GoldenBytes(t *testing.T) {
	cases := []struct {
		name string
		put  func(*protoBuf)
		want string
	}{
		{
			// tag = field<<3 | wiretype = 1<<3|0 = 0x08; 300 as a varint is AC 02.
			name: "varint field",
			put:  func(p *protoBuf) { p.PutUint64(1, 300) },
			want: "08ac02",
		},
		{
			// tag = 2<<3|2 = 0x12, length 2, then the bytes of "hi".
			name: "string field",
			put:  func(p *protoBuf) { p.PutString(2, "hi") },
			want: "12026869",
		},
		{
			// tag = 3<<3|1 = 0x19, then eight little-endian bytes.
			name: "fixed64 field",
			put:  func(p *protoBuf) { p.PutFixed64(3, 1) },
			want: "190100000000000000",
		},
		{
			name: "bool field",
			put:  func(p *protoBuf) { p.PutBool(4, true) },
			want: "2001",
		},
		{
			// A high field number needs a multi-byte tag: 16<<3|2 = 130 = 0x82 0x01.
			name: "high field number",
			put:  func(p *protoBuf) { p.PutString(16, "x") },
			want: "82010178",
		},
		{
			// proto3 omits defaults. Sending them makes the request differ from what
			// Steam's own clients send, which is exactly the kind of difference that gets
			// a request rejected for no visible reason.
			name: "zero values are omitted",
			put: func(p *protoBuf) {
				p.PutUint64(1, 0)
				p.PutString(2, "")
				p.PutBool(3, false)
				p.PutFixed64(4, 0)
				p.PutBytes(5, nil)
			},
			want: "",
		},
		{
			name: "nested message",
			put: func(p *protoBuf) {
				p.PutMessage(9, func(d *protoBuf) { d.PutUint64(2, 1) })
			},
			// tag 9<<3|2 = 0x4a, length 2, then the inner field 2 varint 1 = 10 01.
			want: "4a021001",
		},
		{
			name: "empty nested message is omitted entirely",
			put:  func(p *protoBuf) { p.PutMessage(9, func(d *protoBuf) {}) },
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p protoBuf
			tc.put(&p)
			if got := hex.EncodeToString(p.Bytes()); got != tc.want {
				t.Fatalf("encoded %s, want %s", got, tc.want)
			}
		})
	}
}

func TestDecoder_RoundTrip(t *testing.T) {
	var p protoBuf
	p.PutUint64(1, 1234567890123)
	p.PutBytes(2, []byte{0xde, 0xad, 0xbe, 0xef})
	p.PutString(6, "account_name")
	p.PutFixed64(5, 76561198000000001)

	m, err := decode(p.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got := m.uint64(1); got != 1234567890123 {
		t.Fatalf("uint64 = %d", got)
	}
	if got := m.bytes(2); !bytes.Equal(got, []byte{0xde, 0xad, 0xbe, 0xef}) {
		t.Fatalf("bytes = %x", got)
	}
	if got := m.string(6); got != "account_name" {
		t.Fatalf("string = %q", got)
	}
	// The reader must not care whether a 64-bit value arrived as a varint or a fixed64.
	// Steam declares steamid as fixed64 in some messages and uint64 in others, and getting
	// that wrong on the read side would silently return zero.
	if got := m.uint64(5); got != 76561198000000001 {
		t.Fatalf("fixed64 read back as %d", got)
	}
}

// allowed_confirmations is a repeated nested message, and it is how Steam says which second
// factor an account needs. Misreading it means asking for the wrong kind of code.
func TestDecoder_RepeatedNestedMessages(t *testing.T) {
	var p protoBuf
	p.PutMessage(4, func(c *protoBuf) {
		c.PutUint64(1, guardTypeDeviceCode)
		c.PutString(2, "authenticator")
	})
	p.PutMessage(4, func(c *protoBuf) {
		c.PutUint64(1, guardTypeEmailCode)
		c.PutString(2, "s****@example.test")
	})

	m, err := decode(p.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	confs := m.repeated(4)
	if len(confs) != 2 {
		t.Fatalf("got %d confirmations, want 2", len(confs))
	}
	if confs[0].uint64(1) != guardTypeDeviceCode || confs[0].string(2) != "authenticator" {
		t.Fatalf("first confirmation = %+v", confs[0])
	}
	if confs[1].uint64(1) != guardTypeEmailCode {
		t.Fatalf("second confirmation type = %d", confs[1].uint64(1))
	}
}

// The poll interval arrives as a fixed32 float. Reading it as an integer would give a
// nonsense interval and hammer Steam.
func TestDecoder_Float32(t *testing.T) {
	var p protoBuf
	p.tag(3, wireFixed32)
	bits := math.Float32bits(5.5)
	p.b = append(p.b, byte(bits), byte(bits>>8), byte(bits>>16), byte(bits>>24))

	m, err := decode(p.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got := m.float32(3); got != 5.5 {
		t.Fatalf("float32 = %v, want 5.5", got)
	}
	// A field that is not a fixed32 must read as zero rather than as garbage, so the
	// caller's "no interval given, use the default" path is reached.
	if got := m.float32(99); got != 0 {
		t.Fatalf("missing float32 = %v, want 0", got)
	}
}

// A truncated or malformed body must fail rather than return a half-decoded message that
// looks like a successful login with empty fields.
func TestDecoder_RejectsMalformed(t *testing.T) {
	cases := map[string]string{
		"truncated length-delimited": "1205616263", // claims 5 bytes, supplies 3
		"truncated fixed64":          "1901020304",
		"truncated fixed32":          "1d0102",
		"group wire type":            "0b",
		"dangling tag":               "12",
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			raw, err := hex.DecodeString(h)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decode(raw); err == nil {
				t.Fatalf("decode(%s) succeeded, want an error", h)
			}
		})
	}
}

func TestEResultMapping(t *testing.T) {
	cases := []struct {
		header string
		want   error
	}{
		{"1", nil},
		{"", nil},
		{"not a number", nil},
		{"5", ErrBadCredentials},
		{"84", ErrRateLimited},
		{"87", ErrRateLimited},
		{"88", ErrGuardRejected},
		{"108", ErrSuspended},
		{"8", ErrSuspended},
	}
	for _, tc := range cases {
		got := eresultError(tc.header)
		if tc.want == nil {
			if got != nil {
				t.Fatalf("eresultError(%q) = %v, want nil", tc.header, got)
			}
			continue
		}
		if got == nil || got.Error() != tc.want.Error() {
			t.Fatalf("eresultError(%q) = %v, want %v", tc.header, got, tc.want)
		}
	}
}
