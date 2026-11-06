package server

import (
	"context"
	"log"
	"net"
	"path/filepath"
	"strconv"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/deploy"
	"github.com/dmz006/datawatch/internal/session"
)

// sessionRestartSource reports the running sessions and the held capacity
// leases for the post-restart check. The lease count comes from the daemon's
// capacity ledger; a daemon without a ledger holds no leases, so it reads 0.
type sessionRestartSource struct {
	m      *session.Manager
	ledger *capacity.Ledger
}

func (s sessionRestartSource) RunningSessionIDs(context.Context) ([]string, error) {
	var ids []string
	if s.m == nil {
		return ids, nil
	}
	for _, sess := range s.m.ListSessions() {
		if sess.State == session.StateRunning {
			ids = append(ids, sess.FullID)
		}
	}
	return ids, nil
}

func (s sessionRestartSource) HeldLeaseCount(context.Context) (int, error) {
	if s.ledger == nil {
		return 0, nil
	}
	return len(s.ledger.Snapshot().Leases), nil
}

// restartSource is the one session and lease source used both to record the
// pre-restart baseline and to check it after the restart.
func (s *Server) restartSource() sessionRestartSource {
	return sessionRestartSource{m: s.manager, ledger: s.capacityLedger}
}

// StartDeployResume finishes a deploy the restart interrupted and retries a
// deferred one. It runs once at daemon start, in the background, and never
// blocks serving. A second call starts another pass, which does nothing for a
// state that is already finished.
func (s *Server) StartDeployResume(ctx context.Context) {
	cfg := s.deployConfig()
	if cfg.IsZero() {
		return
	}
	data := mergeDataDir()
	dir := ""
	var mr mergeRunner
	if m, ok := s.autonomousMgr.(mergeRunner); ok && s.autonomousMgr != nil {
		mr = m
		dir = m.MergeBaseRepo()
	}
	store := deploy.FileStore{Path: filepath.Join(data, "deploy", "state.json")}
	resumer := &deploy.Resumer{
		Config: cfg, Dir: dir, DataDir: data,
		Cmd: deploy.ExecCommandRunner{}, Clock: deploy.SystemClock{}, Store: store,
		Sessions: s.restartSource(),
		Ready:    s.waitListening,
		Outcomes: deploy.OutcomeFile{Path: filepath.Join(data, "deploy", "outcomes.jsonl")},
	}
	if mr != nil {
		if q := mr.MergeQuestionStore(); q != nil {
			resumer.Questions = q
		}
	}
	go func() {
		if res, err := resumer.Resume(ctx); err != nil {
			log.Printf("[deploy] resume: %v", err)
		} else if res.Acted {
			log.Printf("[deploy] resume finished: %s", res.Phase)
		}
		runner := newDeployRunner(dir, cfg, s.restartSource())
		if res, err := deploy.RetryDeferred(ctx, store, deploy.SystemClock{}, runner.Retry); err != nil {
			log.Printf("[deploy] retry deferred: %v", err)
		} else if res.Status != deploy.StatusSkipped {
			log.Printf("[deploy] deferred deploy retried: %s %s", res.Status, res.Reason)
		}
	}()
}

// waitListening blocks until the daemon's own port accepts a connection.
func (s *Server) waitListening(ctx context.Context) error {
	if s.cfg == nil || s.cfg.Server.Port == 0 {
		return nil
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(s.cfg.Server.Port))
	for {
		c, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", addr)
		if err == nil {
			_ = c.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// StartDeployResume starts deploy resume on the embedded API server.
func (s *HTTPServer) StartDeployResume(ctx context.Context) {
	if s.api != nil {
		s.api.StartDeployResume(ctx)
	}
}
