package shrink

import "testing"

// fails when the sequence holds a 7 followed (anywhere later) by a 9.
func toy(seq []uint64) Outcome {
	seven := false
	for i, v := range seq {
		if v == 7 {
			seven = true
		}
		if v == 9 && seven {
			return Outcome{Signature: "toy/pair", Events: uint64(i + 1), Effective: seq[:i+1]}
		}
	}
	return Outcome{Events: uint64(len(seq)), Effective: seq}
}

func TestShrinkToyFailure(t *testing.T) {
	seq := make([]uint64, 400)
	for i := range seq {
		seq[i] = uint64(i % 5)
	}
	seq[120], seq[333] = 7, 9
	out, st := Shrink(seq, "toy/pair", toy, 5000)
	if len(out) != 2 || out[0] != 7 || out[1] != 9 {
		t.Fatalf("shrunk to %v (%+v)", out, st)
	}
	if toy(out).Signature != "toy/pair" {
		t.Fatal("shrunk sequence no longer fails")
	}
	if st.EventsAfter >= st.EventsBefore {
		t.Fatalf("events did not shrink: %+v", st)
	}
}

func TestShrinkKeepsPassingSequenceUnchanged(t *testing.T) {
	seq := []uint64{1, 2, 3}
	out, st := Shrink(seq, "toy/pair", toy, 100)
	if len(out) != 3 || st.Kept != 0 {
		t.Fatalf("got %v %+v", out, st)
	}
}

func TestScheduleRoundTrip(t *testing.T) {
	p := t.TempDir() + "/s.json"
	in := &Schedule{Bug: 3, Signature: "a/b", Seed: 9, Choices: []uint64{1, 2, 3}}
	if err := in.Save(p); err != nil {
		t.Fatal(err)
	}
	out, err := Load(p)
	if err != nil || out.Bug != 3 || len(out.Choices) != 3 || out.Signature != "a/b" {
		t.Fatalf("got %+v %v", out, err)
	}
}

// A candidate that still fails but runs longer must be rejected.
func TestShrinkNeverGrows(t *testing.T) {
	run := func(seq []uint64) Outcome {
		if len(seq) == 0 {
			return Outcome{}
		}
		return Outcome{Signature: "s", Events: 100 - uint64(len(seq)), Effective: seq}
	}
	_, st := Shrink([]uint64{5, 5, 5, 5}, "s", run, 100)
	if st.EventsAfter > st.EventsBefore {
		t.Fatalf("events grew: %+v", st)
	}
}
