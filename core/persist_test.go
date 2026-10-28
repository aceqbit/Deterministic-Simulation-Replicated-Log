package core

import (
	"encoding/binary"
	"testing"

	"github.com/aceqbit/seedlog/kv"
	"github.com/aceqbit/seedlog/wal"
)

func TestEncodeRecoverRoundTrip(t *testing.T) {
	c := kv.Command{ClientID: 3, Seq: 4, Op: kv.OpIncr, Key: 2, Value: 1}
	batches := []PersistBatch{
		{HardState: true, Term: 1, Vote: 2},
		{Entries: []Entry{{1, 1, c}, {1, 2, c}, {1, 3, c}}},
		{HardState: true, Term: 2, Vote: None, Truncate: true, TruncateFrom: 3, Entries: []Entry{{2, 3, c}}},
		{Entries: []Entry{{2, 2, c}}}, // implicit truncation of the suffix
		{Truncate: true, TruncateFrom: 9}, // beyond the end: no-op
	}
	var img []byte
	for i := range batches {
		img = EncodeBatch(img, &batches[i])
	}
	st, info := Recover(img, false)
	if info.Corrupt || info.Records != 9 || info.Valid != len(img) || info.Truncated != 0 {
		t.Fatalf("info %+v", info)
	}
	if st.Term != 2 || st.Vote != None || len(st.Entries) != 2 || st.Entries[1].Term != 2 {
		t.Fatalf("state %+v", st)
	}
}

func record(payload []byte) []byte { return wal.AppendRecord(nil, payload) }

func TestRecoverCorrupt(t *testing.T) {
	entry := func(term, idx uint64) []byte {
		b := []byte{recEntry}
		b = binary.LittleEndian.AppendUint64(b, term)
		b = binary.LittleEndian.AppendUint64(b, idx)
		return kv.Command{Op: kv.OpPut}.AppendBinary(b)
	}
	badCmd := entry(1, 1)
	badCmd[17+16] = 99
	tests := []struct {
		name   string
		img    []byte
		reason string
	}{
		{"short hard state", record([]byte{recHardState, 1}), "hard state size"},
		{"short truncate", record([]byte{recTruncate, 1}), "truncate size"},
		{"truncate zero", record(append([]byte{recTruncate}, make([]byte, 8)...)), "truncate index 0"},
		{"short entry", record([]byte{recEntry, 1, 2}), "entry size"},
		{"bad command", record(badCmd), "entry command"},
		{"index zero", record(entry(1, 0)), "entry index gap"},
		{"index gap", record(entry(1, 3)), "entry index gap"},
		{"term decreases", append(record(entry(2, 1)), record(entry(1, 2))...), "entry term decreases"},
		{"unknown type", record([]byte{42}), "record type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, info := Recover(tt.img, false)
			if !info.Corrupt || info.Reason != tt.reason {
				t.Fatalf("info %+v", info)
			}
		})
	}
}

func TestRecoverTornTail(t *testing.T) {
	b := PersistBatch{Entries: []Entry{{1, 1, kv.Command{Op: kv.OpPut, Value: 5}}, {1, 2, kv.Command{Op: kv.OpPut, Value: 6}}}}
	img := EncodeBatch(nil, &b)
	for i := len(img) - 20; i < len(img); i++ {
		img[i] = 0 // zero the tail of the second entry, as a torn sector would
	}
	st, info := Recover(img, false)
	if len(st.Entries) != 1 || info.Truncated == 0 || info.TornAccepted {
		t.Fatalf("correct recovery: %d entries %+v", len(st.Entries), info)
	}
	st, info = Recover(img, true)
	if len(st.Entries) != 2 || !info.TornAccepted || st.Entries[1].Cmd.Value == 6 {
		t.Fatalf("bug 10 recovery: %+v %+v", st.Entries, info)
	}
}
