package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aceqbit/seedlog/core"
	"github.com/aceqbit/seedlog/kv"
)

// Handler returns the client API:
//
//	PUT  /kv/{key}?value=V
//	GET  /kv/{key}
//	POST /kv/{key}/cas?expect=E&value=V
//	POST /kv/{key}/incr?delta=D
//	GET  /healthz, /readyz, /metrics
//
// A client may pass X-Client-ID and X-Seq so a retry applies once; otherwise
// the node assigns both.
func (n *Node) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprintln(w, "ok") })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !n.Ready() {
			http.Error(w, "no leader known", http.StatusServiceUnavailable)
			return
		}
		_, _ = fmt.Fprintf(w, "leader %d term %d\n", n.Leader(), n.Term())
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "seedlog_raft_term %d\nseedlog_raft_commit_index %d\nseedlog_raft_leader %d\n",
			n.term.Load(), n.commit.Load(), n.leader.Load())
	})
	mux.HandleFunc("/kv/", n.serveKV)
	return mux
}

func (n *Node) serveKV(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/kv/"), "/")
	key, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		http.Error(w, "key must be an unsigned 32-bit integer", http.StatusBadRequest)
		return
	}
	cmd := kv.Command{Key: uint32(key)}
	q := r.URL.Query()
	num := func(name string) (int64, bool) {
		v, err := strconv.ParseInt(q.Get(name), 10, 64)
		if err != nil {
			http.Error(w, "bad "+name, http.StatusBadRequest)
			return 0, false
		}
		return v, true
	}
	ok := true
	switch {
	case len(parts) == 1 && r.Method == http.MethodPut:
		cmd.Op = kv.OpPut
		cmd.Value, ok = num("value")
	case len(parts) == 1 && r.Method == http.MethodGet:
		cmd.Op = kv.OpGet
	case len(parts) == 2 && parts[1] == "cas" && r.Method == http.MethodPost:
		cmd.Op = kv.OpCAS
		if cmd.Expect, ok = num("expect"); ok {
			cmd.Value, ok = num("value")
		}
	case len(parts) == 2 && parts[1] == "incr" && r.Method == http.MethodPost:
		cmd.Op = kv.OpIncr
		cmd.Value, ok = num("delta")
	default:
		http.Error(w, "unsupported route", http.StatusNotFound)
		return
	}
	if !ok {
		return
	}
	if v := r.Header.Get("X-Client-ID"); v != "" {
		cmd.ClientID, _ = strconv.ParseUint(v, 10, 64)
		cmd.Seq, _ = strconv.ParseUint(r.Header.Get("X-Seq"), 10, 64)
	}
	if cmd.ClientID == 0 || cmd.Seq == 0 {
		cmd.ClientID, cmd.Seq = n.NextClientID(), 1
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	rep, err := n.Do(ctx, cmd)
	switch {
	case errors.Is(err, ErrNotLeader):
		if rep.Leader != core.None {
			w.Header().Set("X-Leader", strconv.Itoa(rep.Leader))
		}
		http.Error(w, "not leader", http.StatusMisdirectedRequest)
	case err != nil:
		http.Error(w, err.Error(), http.StatusGatewayTimeout)
	default:
		_, _ = fmt.Fprintf(w, "{\"ok\":%t,\"value\":%d,\"index\":%d,\"term\":%d}\n", rep.Result.OK, rep.Result.Value, rep.Index, rep.Term)
	}
}
