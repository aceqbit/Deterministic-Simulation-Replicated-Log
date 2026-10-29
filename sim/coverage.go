package sim

// Fault kinds. The set is closed: it is a metric attribute.
const (
	FaultDrop = iota
	FaultDup
	FaultReorder
	FaultPartition
	FaultCrash
	FaultPause
	FaultSkew
	FaultTorn
	FaultLostFsync
	NumFaults
)

// FaultNames names the fault kinds in order.
var FaultNames = [NumFaults]string{"drop", "dup", "reorder", "partition", "crash", "pause", "skew", "torn", "lost_fsync"}

// Protocol coverage points.
const (
	CovElection = iota
	CovElectionAfterCrash
	CovSplitVote
	CovLeaderElected
	CovLeaderChange
	CovLeaderStepDown
	CovCandidateStepDown
	CovLogTruncation
	CovCommit
	CovCommitAfterLeaderChange
	CovVoteGranted
	CovVoteRejectedLog
	CovVoteRejectedVoted
	CovAppendRejectMissing
	CovAppendRejectConflict
	CovStaleTerm
	CovBatch
	CovDedupHit
	CovNotLeader
	CovGated
	CovHeartbeat
	CovRestoreNonEmpty
	CovRestoreEmpty
	CovCrash
	CovCrashLeader
	CovCrashTwoDown
	CovCrashMajorityDown
	CovPause
	CovPausedLeader
	CovSkew
	CovDrop
	CovDup
	CovReorder
	CovPartitionSym
	CovPartitionAsym
	CovPartitionHeal
	CovTornWrite
	CovLostWrite
	CovTornTailRecovered
	CovClientRetry
	CovCASOK
	CovCASFail
	CovIncr
	CovPut
	CovGet
	CovRestartCatchUp
	NumCov
)

// CovNames names the coverage points in order.
var CovNames = [NumCov]string{
	"election", "election_after_crash", "split_vote", "leader_elected", "leader_change",
	"leader_step_down", "candidate_step_down", "log_truncation", "commit",
	"commit_after_leader_change", "vote_granted", "vote_rejected_log", "vote_rejected_voted",
	"append_reject_missing", "append_reject_conflict", "stale_term", "batch", "dedup_hit",
	"not_leader", "gated_output", "heartbeat", "restore_nonempty", "restore_empty", "crash",
	"crash_leader", "crash_two_down", "crash_majority_down", "pause", "paused_leader", "skew",
	"drop", "dup", "reorder", "partition_sym", "partition_asym", "partition_heal", "torn_write",
	"lost_write", "torn_tail_recovered", "client_retry", "cas_ok", "cas_fail", "incr", "put",
	"get", "restart_catch_up",
}
