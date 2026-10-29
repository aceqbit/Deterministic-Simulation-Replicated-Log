package runtime

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/aceqbit/seedlog/check"
	"github.com/aceqbit/seedlog/core"
	"github.com/aceqbit/seedlog/kv"
	"github.com/aceqbit/seedlog/wal"
)

func freeAddrs(t *testing.T, n int) []string {
	t.Helper()
	var out []string
	for i := 0; i < n; i++ {
		c, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, c.LocalAddr().String())
		_ = c.Close()
	}
	return out
}

type testCluster struct {
	t     *testing.T
	peers []string
	store []wal.Storage
	nodes []*Node
}

func newTestCluster(t *testing.T, n int, files bool) *testCluster {
	c := &testCluster{t: t, peers: freeAddrs(t, n), nodes: make([]*Node, n)}
	for i := 0; i < n; i++ {
		if files {
			f, err := OpenFile(fmt.Sprintf("%s/n%d.wal", t.TempDir(), i))
			if err != nil {
				t.Fatal(err)
			}
			c.store = append(c.store, f)
		} else {
			c.store = append(c.store, &wal.Mem{})
		}
	}
	for i := range c.nodes {
		c.start(i)
	}
	t.Cleanup(func() {
		for _, nd := range c.nodes {
			if nd != nil {
				nd.Stop()
			}
		}
		for _, s := range c.store {
			if f, ok := s.(*FileStorage); ok {
				_ = f.Close()
			}
		}
	})
	return c
}

func (c *testCluster) start(i int) {
	nd, err := Start(Config{ID: i, Peers: c.peers, Storage: c.store[i], Heartbeat: 20 * time.Millisecond, Election: 100 * time.Millisecond})
	if err != nil {
		c.t.Fatal(err)
	}
	c.nodes[i] = nd
}

func (c *testCluster) leader(timeout time.Duration) *Node {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, nd := range c.nodes {
			if nd != nil && nd.IsLeader() {
				return nd
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	c.t.Fatal("no leader")
	return nil
}

func do(c *testCluster, cmd kv.Command) core.Reply {
	for attempt := 0; attempt < 100; attempt++ {
		ld := c.leader(5 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		rep, err := ld.Do(ctx, cmd)
		cancel()
		if err == nil {
			return rep
		}
	}
	c.t.Fatal("command never completed")
	return core.Reply{}
}

func TestClusterReplicatesAndSurvivesLeaderRestart(t *testing.T) {
	c := newTestCluster(t, 3, true)
	var h check.History
	seq := uint64(0)
	run := func(op kv.OpKind, key uint32, v int64) {
		seq++
		cmd := kv.Command{ClientID: 7, Seq: seq, Op: op, Key: key, Value: v}
		i := h.Invoke(check.Op{ClientID: 7, Seq: seq, Kind: op, Key: key, Value: v, Call: time.Now().UnixNano()})
		rep := do(c, cmd)
		h.Complete(i, time.Now().UnixNano(), rep.Result)
	}
	for k := uint32(0); k < 4; k++ {
		run(kv.OpPut, k, int64(k)+10)
		run(kv.OpIncr, k, 1)
		run(kv.OpGet, k, 0)
	}
	old := c.leader(time.Second)
	id := old.cfg.ID
	old.Stop()
	c.nodes[id] = nil
	c.start(id) // restarts from its file WAL
	for k := uint32(0); k < 4; k++ {
		run(kv.OpGet, k, 0)
		run(kv.OpIncr, k, 2)
		run(kv.OpGet, k, 0)
	}
	if got := check.Linearizable(h.Ops, 10*time.Second); got != check.LinOK {
		t.Fatalf("history of %d ops is %v", len(h.Ops), got)
	}
	last := h.Ops[len(h.Ops)-1]
	if last.Out != int64(last.Key)+10+1+2 {
		t.Fatalf("final value of key %d = %d", last.Key, last.Out)
	}
}

func TestReadyzNeedsLeader(t *testing.T) {
	c := newTestCluster(t, 3, false)
	c.leader(5 * time.Second)
	deadline := time.Now().Add(5 * time.Second)
	for !c.nodes[0].Ready() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !c.nodes[0].Ready() {
		t.Fatal("follower never learned the leader")
	}
}
