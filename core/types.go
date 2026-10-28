package core

import (
	"errors"

	"github.com/aceqbit/seedlog/kv"
)

// None is the node ID meaning "no node".
const None = -1

// Role is a node's Raft role.
type Role uint8

// Roles.
const (
	Follower Role = iota
	Candidate
	Leader
)

// String returns the role name used in metric attributes.
func (r Role) String() string {
	switch r {
	case Follower:
		return "follower"
	case Candidate:
		return "candidate"
	case Leader:
		return "leader"
	}
	return "invalid"
}

// TimerKind names the core's timers.
type TimerKind uint8

// Timers.
const (
	TimerElection TimerKind = iota
	TimerHeartbeat
	NumTimers
)

// Bugs holds the ten injectable faults of the study. All are false in a
// correct build; each is read at exactly one hook point.
type Bugs struct {
	VoteBeforePersist bool // 1: vote reply leaves before the vote is durable
	SkipUptodateCheck bool // 2: vote granted without the log up-to-date check
	Fig8Commit        bool // 3: leader commits by counting replicas of old-term entries
	TruncateAlways    bool // 4: follower truncates after prevIndex even when entries match
	NoStepdown        bool // 5: leader adopts a higher term but stays leader
	CommitPastEnd     bool // 6: follower commit index set to leaderCommit unbounded
	StaleLeaderRead   bool // 7: leader serves GET from local state, bypassing the log
	AckBeforeFsync    bool // 8: replication acks and leader self-match before fsync
	DoubleApplyRetry  bool // 9: session dedup disabled, a retry applies twice
	AcceptTornTail    bool // 10: WAL recovery accepts a checksum-failing tail record
}

// BugNames lists the flags in study order; BugNames[n-1] is bug n.
var BugNames = [10]string{
	"vote_before_persist", "skip_uptodate_check", "fig8_commit", "truncate_always",
	"no_stepdown", "commit_past_end", "stale_leader_read", "ack_before_fsync",
	"double_apply_retry", "accept_torn_tail",
}

// ErrBug is returned for a bug number outside 0..10.
var ErrBug = errors.New("core: bug number must be 0..10")

// BugByNumber returns a Bugs value with only bug n set; n == 0 is no bug.
func BugByNumber(n int) (Bugs, error) {
	var b Bugs
	switch n {
	case 0:
	case 1:
		b.VoteBeforePersist = true
	case 2:
		b.SkipUptodateCheck = true
	case 3:
		b.Fig8Commit = true
	case 4:
		b.TruncateAlways = true
	case 5:
		b.NoStepdown = true
	case 6:
		b.CommitPastEnd = true
	case 7:
		b.StaleLeaderRead = true
	case 8:
		b.AckBeforeFsync = true
	case 9:
		b.DoubleApplyRetry = true
	case 10:
		b.AcceptTornTail = true
	default:
		return b, ErrBug
	}
	return b, nil
}

// Config is a node's static configuration. Durations are in nanoseconds.
type Config struct {
	ID             int
	N              int   // cluster size; node IDs are 0..N-1
	ElectionBase   int64 // minimum election timeout
	ElectionJitter int64 // election timeout is base + [0, jitter]
	Heartbeat      int64
	MaxBatch       int // entries per AppendEntries; 0 means 64
	Bugs           Bugs
}

// Entry is one log entry. Index 0 is a sentinel and never stored.
type Entry struct {
	Term  uint64
	Index uint64
	Cmd   kv.Command
}

// MsgType is a Raft RPC type.
type MsgType uint8

// Message types.
const (
	MsgRequestVote MsgType = iota + 1
	MsgVoteReply
	MsgAppend
	MsgAppendReply
)

// Message is a Raft RPC or reply. Fields unused by a type are zero.
type Message struct {
	Type     MsgType
	From, To int
	Term     uint64
	// RequestVote
	LastIndex, LastTerm uint64
	// VoteReply, AppendReply
	Granted bool // vote granted; append success
	// AppendEntries
	PrevIndex, PrevTerm uint64
	Entries             []Entry
	Commit              uint64
	// AppendReply
	Match uint64 // success: last index known to match
	Hint  uint64 // failure: next index the leader should try
}

// EventKind identifies an input to Step.
type EventKind uint8

// Event kinds.
const (
	EvStart EventKind = iota
	EvTimer
	EvMessage
	EvPersistDone
	EvClient
)

// Event is one input to Step.
type Event struct {
	Kind  EventKind
	Timer TimerKind
	Msg   Message
	Token uint64
	Cmd   kv.Command
}

// EffectKind identifies an output of Step.
type EffectKind uint8

// Effect kinds.
const (
	EffSend EffectKind = iota
	EffSetTimer
	EffPersist
	EffReply
)

// ReplyStatus is the status of a client reply.
type ReplyStatus uint8

// Reply statuses.
const (
	ReplyOK ReplyStatus = iota
	ReplyNotLeader
)

// Reply answers a client command.
type Reply struct {
	ClientID, Seq uint64
	Status        ReplyStatus
	Result        kv.Result
	Leader        int    // NotLeader: leader hint or None
	Index, Term   uint64 // OK: log position the command committed at (0 for local reads)
}

// PersistBatch is the durable state change of one Step, written in order:
// hard state, truncation, then entries.
type PersistBatch struct {
	HardState    bool
	Term         uint64
	Vote         int
	Truncate     bool
	TruncateFrom uint64 // entries with Index >= TruncateFrom are removed
	Entries      []Entry
}

// Empty reports whether the batch carries no change.
func (b *PersistBatch) Empty() bool { return !b.HardState && !b.Truncate && len(b.Entries) == 0 }

// Effect is one output of Step.
type Effect struct {
	Kind   EffectKind
	Msg    Message
	Timer  TimerKind
	Base   int64
	Jitter int64
	Batch  PersistBatch
	Token  uint64
	Reply  Reply
}

// ObsKind identifies an Observation.
type ObsKind uint8

// Observation kinds. The comment lists the fields each kind sets.
const (
	ObsState          ObsKind = iota // Term, A=role
	ObsAppend                        // Index, Term, A=command hash
	ObsTruncate                      // Index: first removed index
	ObsCommit                        // Index: new commit index
	ObsApply                         // Index, Term, A=command hash
	ObsRestore                       // Index: last recovered index, Term
	ObsElectionStart                 // Term
	ObsVoteGranted                   // Term, A=candidate
	ObsVoteRejectedLog               // Term, A=candidate
	ObsVoteRejectedVoted             // Term, A=candidate
	ObsAppendReject                  // Index: prevIndex, A=1 missing, 2 term mismatch
	ObsStaleTerm                     // Term of the stale message
	ObsHeartbeat                     // Term
	ObsBatch                         // Index: entries in one AppendEntries
	ObsDedupHit                      // Index
	ObsNotLeader                     //
	ObsGated                         // A=held outputs
	ObsLocalRead                     // A=key
	ObsStepDown                      // Term, A=old role
	ObsRPC                           // A=MsgType of request, B=0 ok, 1 reject, 2 conflict
	ObsCommitOldTerm                 // Index committed while it held an older term
	NumObsKinds
)

// Observation is a side record of what Step did. Hosts turn observations into
// checker input, coverage points and telemetry; they never feed back into Step.
type Observation struct {
	Kind  ObsKind
	Node  int
	Term  uint64
	Index uint64
	A, B  uint64
}
