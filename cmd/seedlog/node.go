package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/aceqbit/seedlog/runtime"
)

// cmdNode runs one replica of a real cluster: UDP between peers, HTTP for clients.
func cmdNode(args []string) error {
	fs := flag.NewFlagSet("node", flag.ExitOnError)
	id := fs.Int("id", 0, "node ID (index into --peers)")
	peers := fs.String("peers", "", "comma-separated UDP addresses of every node")
	httpAddr := fs.String("http", ":8080", "client API listen address")
	dir := fs.String("data", "data", "directory for the write-ahead log")
	_ = fs.Parse(args)
	list := strings.Split(*peers, ",")
	if *peers == "" || *id < 0 || *id >= len(list) {
		return fmt.Errorf("node: --id must index --peers")
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return err
	}
	st, err := runtime.OpenFile(filepath.Join(*dir, fmt.Sprintf("node%d.wal", *id)))
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	n, err := runtime.Start(runtime.Config{ID: *id, Peers: list, Storage: st, Logger: logger})
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: *httpAddr, Handler: n.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http", "err", err)
		}
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, os.Interrupt)
	<-sig
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	n.Stop() // flush: the loop finishes its step and the log is synced
	return nil
}
