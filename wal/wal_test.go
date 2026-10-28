package wal

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func image(payloads ...string) []byte {
	var b []byte
	for _, p := range payloads {
		b = AppendRecord(b, []byte(p))
	}
	return b
}

func TestRecover(t *testing.T) {
	full := image("a", "bb", "ccc")
	tests := []struct {
		name      string
		buf       []byte
		opt       Options
		records   int
		valid     int
		truncated int
		torn      bool
	}{
		{"empty", nil, Options{}, 0, 0, 0, false},
		{"clean", full, Options{}, 3, len(full), 0, false},
		{"short header", full[:len(full)-len("ccc")-HeaderSize+3], Options{}, 2, 19, 3, false},
		{"short payload", full[:len(full)-1], Options{}, 2, 19, len(full) - 1 - 19, false},
		{"zero tail", append(append([]byte{}, full...), make([]byte, 64)...), Options{}, 3, len(full), 64, false},
		{"huge length", append(append([]byte{}, full...), 0xff, 0xff, 0xff, 0x7f, 0, 0, 0, 0, 1), Options{}, 3, len(full), 9, false},
	}
	torn := append([]byte{}, full...)
	torn[len(torn)-1] ^= 0xff
	tests = append(tests,
		struct {
			name      string
			buf       []byte
			opt       Options
			records   int
			valid     int
			truncated int
			torn      bool
		}{"bad crc tail", torn, Options{}, 2, 19, len(full) - 19, false},
		struct {
			name      string
			buf       []byte
			opt       Options
			records   int
			valid     int
			truncated int
			torn      bool
		}{"bug accepts torn tail", torn, Options{AcceptTornTail: true}, 3, len(full), 0, true},
	)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Recover(tt.buf, tt.opt)
			if len(r.Records) != tt.records || r.Valid != tt.valid || r.Truncated != tt.truncated || r.TornAccepted != tt.torn {
				t.Fatalf("got records=%d valid=%d truncated=%d torn=%v", len(r.Records), r.Valid, r.Truncated, r.TornAccepted)
			}
		})
	}
}

func TestRecoverStopsAtMiddleCorruption(t *testing.T) {
	b := image("one", "two", "three")
	b[HeaderSize+3+HeaderSize] ^= 1 // flip a byte of "two"
	r := Recover(b, Options{})
	if len(r.Records) != 1 || string(r.Records[0]) != "one" {
		t.Fatalf("records %q", r.Records)
	}
}

func TestMem(t *testing.T) {
	var m Mem
	var s Storage = &m
	_ = s.Append([]byte("abc"))
	_ = s.Sync()
	if err := s.Truncate(9); err != ErrTruncate {
		t.Fatal("truncate past end")
	}
	if err := s.Truncate(2); err != nil {
		t.Fatal(err)
	}
	got, _ := s.ReadAll()
	if !bytes.Equal(got, []byte("ab")) {
		t.Fatalf("got %q", got)
	}
}

// FuzzRecover checks that Recover never panics and that every record it
// accepts is checksum-valid and lies inside the valid prefix.
func FuzzRecover(f *testing.F) {
	f.Add(image("a", "bb", "ccc"))
	f.Add(image("x")[:5])
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		r := Recover(b, Options{})
		if r.Valid+r.Truncated != len(b) || r.Valid < 0 || r.Valid > len(b) {
			t.Fatalf("valid=%d truncated=%d len=%d", r.Valid, r.Truncated, len(b))
		}
		off := 0
		for _, p := range r.Records {
			n := int(binary.LittleEndian.Uint32(b[off:]))
			if n != len(p) || Checksum(p) != binary.LittleEndian.Uint32(b[off+4:]) {
				t.Fatal("accepted an invalid record")
			}
			off += HeaderSize + n
		}
		if off != r.Valid {
			t.Fatalf("records end at %d, valid %d", off, r.Valid)
		}
		again := Recover(b[:r.Valid], Options{})
		if len(again.Records) != len(r.Records) || again.Truncated != 0 {
			t.Fatal("valid prefix must recover identically")
		}
		_ = Recover(b, Options{AcceptTornTail: true})
	})
}
