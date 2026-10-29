package sim

import (
	"bufio"
	"fmt"
	"os"
	"testing"
)

// TestGoldenHashes fails on any change to the trace of seeds 1..100. After an
// intended simulator change, regenerate with `make golden` and review the diff.
func TestGoldenHashes(t *testing.T) {
	f, err := os.Open("../testdata/golden.txt")
	if err != nil {
		t.Skip("no golden file yet: run make golden")
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	cfg := Reference()
	n := 0
	for sc.Scan() {
		var seed uint64
		var hash string
		if _, err := fmt.Sscanf(sc.Text(), "%d %s", &seed, &hash); err != nil {
			t.Fatalf("bad golden line %q", sc.Text())
		}
		if got := fmt.Sprintf("%016x", run(seed, cfg).Hash); got != hash {
			t.Errorf("seed %d: hash %s, golden %s", seed, got, hash)
		}
		n++
	}
	if n != 100 {
		t.Fatalf("golden file has %d seeds, want 100", n)
	}
}
