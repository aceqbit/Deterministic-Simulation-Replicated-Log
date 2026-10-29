package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aceqbit/seedlog/core"
	"github.com/aceqbit/seedlog/kv"
	"github.com/aceqbit/seedlog/wal"
)

// Config configures one runtime node.
type Config struct {
	ID        int
	Peers     []string // UDP addresses, indexed by node ID
	Storage   wal.Storage
	Bugs      core.Bugs
	Heartbeat time.Duration
	Election  time.Duration // base election timeout; jitter is the same again
	Logger    *slog.Logger
	// Seed seeds the election-jitter PRNG; zero uses the node ID.
	Seed uint64
}

type pendingKey struct{ client, seq uint64 }

// Node is a running replica: one event-loop goroutine owns the core.
type Node struct {
	cfg   Config
	core  *core.Node
	conn  *net.UDPConn
	peers []*net.UDPAddr
	rng   *rand.Rand
	log   *slog.Logger

	events chan core.Event
	quit   chan struct{}
	done   chan struct{}
	timers [core.NumTimers]*time.Timer

	mu      sync.Mutex
	pending map[pendingKey]chan core.Reply

	leader  atomic.Int64
	term    atomic.Uint64
	commit  atomic.Uint64
	clients atomic.Uint64
	wg      sync.WaitGroup
	once    sync.Once
}

// ErrNotLeader is returned by Do when this node is not the leader.
var ErrNotLeader = errors.New("runtime: not leader")

// ErrTimeout is returned by Do when no reply arrived in time.
var ErrTimeout = errors.New("runtime: request timed out")

// Start recovers the node from its storage, opens its UDP socket and starts
// the event loop.
func Start(cfg Config) (*Node, error) {
	if cfg.Heartbeat == 0 {
		cfg.Heartbeat = 50 * time.Millisecond
	}
	if cfg.Election == 0 {
		cfg.Election = 150 * time.Millisecond
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	img, err := cfg.Storage.ReadAll()
	if err != nil {
		return nil, err
	}
	st, info := core.Recover(img, cfg.Bugs.AcceptTornTail)
	if info.Corrupt {
		return nil, fmt.Errorf("runtime: corrupt log: %s", info.Reason)
	}
	if info.Truncated > 0 {
		if err := cfg.Storage.Truncate(int64(info.Valid)); err != nil {
			return nil, err
		}
		cfg.Logger.Warn("dropped torn wal tail", "node", cfg.ID, "bytes", info.Truncated)
	}
	n := &Node{cfg: cfg, log: cfg.Logger.With("node", cfg.ID),
		events: make(chan core.Event, 4096), quit: make(chan struct{}), done: make(chan struct{}),
		pending: map[pendingKey]chan core.Reply{}}
	seed := cfg.Seed
	if seed == 0 {
		seed = uint64(cfg.ID) + 1
	}
	n.rng = rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	for _, p := range cfg.Peers {
		a, err := net.ResolveUDPAddr("udp", p)
		if err != nil {
			return nil, err
		}
		n.peers = append(n.peers, a)
	}
	n.conn, err = net.ListenUDP("udp", n.peers[cfg.ID])
	if err != nil {
		return nil, err
	}
	n.core = core.NewNode(core.Config{ID: cfg.ID, N: len(cfg.Peers),
		ElectionBase: int64(cfg.Election), ElectionJitter: int64(cfg.Election),
		Heartbeat: int64(cfg.Heartbeat), Bugs: cfg.Bugs}, st)
	n.leader.Store(core.None)
	n.events <- core.Event{Kind: core.EvStart}
	n.wg.Add(2)
	go n.readLoop()
	go n.loop()
	return n, nil
}

// Leader returns the leader this node knows, or core.None.
func (n *Node) Leader() int { return int(n.leader.Load()) }

// Term returns the node's current term.
func (n *Node) Term() uint64 { return n.term.Load() }

// IsLeader reports whether this node believes it leads.
func (n *Node) IsLeader() bool { return n.Leader() == n.cfg.ID }

// Ready reports whether a leader is known.
func (n *Node) Ready() bool { return n.Leader() != core.None }

// Stop shuts the node down (the SIGTERM path). The event loop finishes its
// in-flight step, so every Persist it issued has already been synced.
func (n *Node) Stop() {
	n.once.Do(func() {
		close(n.quit)
		_ = n.conn.Close()
		n.wg.Wait()
		n.mu.Lock()
		for _, t := range n.timers {
			if t != nil {
				t.Stop()
			}
		}
		n.mu.Unlock()
		_ = n.cfg.Storage.Sync()
		close(n.done)
	})
	<-n.done
}

func (n *Node) readLoop() {
	defer n.wg.Done()
	buf := make([]byte, 64<<10)
	for {
		k, _, err := n.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-n.quit:
				return
			default:
				continue
			}
		}
		m, err := Unmarshal(buf[:k])
		if err != nil || m.To != n.cfg.ID || m.From < 0 || m.From >= len(n.peers) {
			continue
		}
		select {
		case n.events <- core.Event{Kind: core.EvMessage, Msg: m}:
		case <-n.quit:
			return
		}
	}
}

func (n *Node) loop() {
	defer n.wg.Done()
	for {
		select {
		case <-n.quit:
			return
		case ev := <-n.events:
			n.step(ev)
		}
	}
}

func (n *Node) step(ev core.Event) {
	effs, _ := n.core.Step(ev)
	n.term.Store(n.core.Term())
	n.leader.Store(int64(n.core.Leader()))
	n.commit.Store(n.core.Commit())
	// Persist effects run first, in order and synchronously; each completion
	// is queued back as a PersistDone event. Sends and replies in effs were
	// already gated by the core behind their token.
	var done []uint64
	for i := range effs {
		if e := &effs[i]; e.Kind == core.EffPersist {
			if err := n.persist(e); err != nil {
				n.log.Error("persist failed", "err", err)
				panic(err) // a node that cannot persist must not acknowledge anything
			}
			done = append(done, e.Token)
		}
	}
	for i := range effs {
		e := &effs[i]
		switch e.Kind {
		case core.EffSend:
			if e.Msg.To >= 0 && e.Msg.To < len(n.peers) {
				_, _ = n.conn.WriteToUDP(Marshal(&e.Msg), n.peers[e.Msg.To])
			}
		case core.EffSetTimer:
			n.setTimer(e.Timer, e.Base, e.Jitter)
		case core.EffReply:
			n.deliver(e.Reply)
		}
	}
	for _, tok := range done {
		select {
		case n.events <- core.Event{Kind: core.EvPersistDone, Token: tok}:
		default:
			go func(tok uint64) {
				select {
				case n.events <- core.Event{Kind: core.EvPersistDone, Token: tok}:
				case <-n.quit:
				}
			}(tok)
		}
	}
}

func (n *Node) persist(e *core.Effect) error {
	if err := n.cfg.Storage.Append(core.EncodeBatch(nil, &e.Batch)); err != nil {
		return err
	}
	return n.cfg.Storage.Sync()
}

func (n *Node) setTimer(k core.TimerKind, base, jitter int64) {
	d := time.Duration(base)
	if jitter > 0 {
		d += time.Duration(n.rng.Int64N(jitter + 1))
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if t := n.timers[k]; t != nil {
		t.Stop()
	}
	n.timers[k] = time.AfterFunc(d, func() {
		select {
		case n.events <- core.Event{Kind: core.EvTimer, Timer: k}:
		case <-n.quit:
		}
	})
}

func (n *Node) deliver(r core.Reply) {
	n.mu.Lock()
	ch := n.pending[pendingKey{r.ClientID, r.Seq}]
	n.mu.Unlock()
	if ch != nil {
		select {
		case ch <- r:
		default:
		}
	}
}

// Do submits a command and waits for its reply. A retry with the same
// (ClientID, Seq) applies once.
func (n *Node) Do(ctx context.Context, cmd kv.Command) (core.Reply, error) {
	key := pendingKey{cmd.ClientID, cmd.Seq}
	ch := make(chan core.Reply, 4)
	n.mu.Lock()
	n.pending[key] = ch
	n.mu.Unlock()
	defer func() {
		n.mu.Lock()
		delete(n.pending, key)
		n.mu.Unlock()
	}()
	select {
	case n.events <- core.Event{Kind: core.EvClient, Cmd: cmd}:
	case <-ctx.Done():
		return core.Reply{}, ErrTimeout
	}
	select {
	case r := <-ch:
		if r.Status == core.ReplyNotLeader {
			return r, ErrNotLeader
		}
		return r, nil
	case <-ctx.Done():
		return core.Reply{}, ErrTimeout
	}
}

// NextClientID returns a fresh client ID unique to this node.
func (n *Node) NextClientID() uint64 { return n.clients.Add(1) + uint64(n.cfg.ID+1)<<48 }
