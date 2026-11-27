// The schedule command-line verbs. They go through the running daemon, which
// holds the schedules in memory: a schedule written straight to the file never
// fired. The file is touched only when the daemon is proven to be down.

package main

import (
	"bytes"
	crypto_tls "crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/secfile"
	"github.com/dmz006/datawatch/internal/session"
)

// scheduleRequest calls the daemon. An error means no HTTP answer came back.
func scheduleRequest(cfg *config.Config, method, path string, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, loopbackBaseURL(cfg)+path, rd)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cfg.Server.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
	}
	// An HTTP to HTTPS redirect on the loopback port lands on a self-signed
	// certificate.
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &crypto_tls.Config{InsecureSkipVerify: true}, // #nosec G402 -- accepted risk: the daemon's self-signed certificate is not verified, for any server address in use
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	reply, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, reply, nil
}

// secureInstall reports whether this data folder is used with encryption.
func secureInstall(cfg *config.Config) bool {
	if secureMode || isConfigEncrypted(resolveConfigPath()) {
		return true
	}
	if _, err := os.Stat(filepath.Join(expandHome(cfg.DataDir), "enc.salt")); err == nil {
		return true
	}
	raw, err := os.ReadFile(schedStorePath(cfg))
	return err == nil && secfile.IsEncrypted(raw)
}

// offlineScheduleStore opens schedule.json for a command the daemon did not
// answer (asked is that failure). It does so only when the daemon is proven to
// be down, by taking the lock a running daemon holds; the caller must call
// release when it is done with the file.
func offlineScheduleStore(cfg *config.Config, asked error) (*session.ScheduleStore, func(), error) {
	if serverURL != "" {
		return nil, nil, fmt.Errorf("the daemon at %s did not answer, and its schedules are not this host's file; nothing was written: %w", serverURL, asked)
	}
	if secureInstall(cfg) {
		return nil, nil, fmt.Errorf("this is a secure install: schedules can only be read or changed through the running daemon, which did not answer; nothing was written: %w", asked)
	}
	dir := filepath.Dir(schedStorePath(cfg))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("data folder: %w", err)
	}
	release, free, err := session.TryDaemonLock(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("the daemon did not answer and it cannot be told whether it is running (%v); nothing was written: %w", err, asked)
	}
	if !free {
		return nil, nil, fmt.Errorf("datawatch is running but did not answer at %s; the file was not touched (check the schedule list before trying again): %w", loopbackBaseURL(cfg), asked)
	}
	store, err := session.NewScheduleStore(schedStorePath(cfg))
	if err != nil {
		release()
		return nil, nil, fmt.Errorf("open schedule store: %w", err)
	}
	fmt.Println("datawatch is not running: using schedule.json directly.")
	return store, release, nil
}

func scheduleRefusal(verb string, status int, reply []byte) error {
	return fmt.Errorf("schedule %s: HTTP %d: %s", verb, status, strings.TrimSpace(string(reply)))
}

func runScheduleAdd(cfg *config.Config, sessionID, sessionName, command, at, cronExpr, scheduleName string) error {
	// Validate cron if provided
	if cronExpr != "" {
		if err := session.ValidateCron(cronExpr); err != nil {
			return fmt.Errorf("invalid cron expression %q: %w", cronExpr, err)
		}
	}

	var runAt time.Time
	var err error
	if cronExpr != "" && at == "" {
		// Compute initial run time from cron
		runAt, err = session.CronNext(cronExpr, time.Now())
		if err != nil {
			return fmt.Errorf("cron next: %w", err)
		}
	} else if at != "" && at != "now" {
		// Try HH:MM first
		if t, err2 := time.Parse("15:04", at); err2 == nil {
			now := time.Now()
			runAt = time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
			if runAt.Before(now) {
				runAt = runAt.Add(24 * time.Hour)
			}
		} else {
			// Try RFC3339
			runAt, err = time.Parse(time.RFC3339, at)
			if err != nil {
				return fmt.Errorf("invalid --at value %q: use 'now', 'HH:MM', or RFC3339 timestamp", at)
			}
		}
	}

	body := map[string]any{
		"session_id": sessionID, "session_name": sessionName, "command": command,
		"cron_expr": cronExpr, "schedule_name": scheduleName,
	}
	if !runAt.IsZero() {
		body["run_at"] = runAt.Format(time.RFC3339)
	}
	var sc session.ScheduledCommand
	status, reply, asked := scheduleRequest(cfg, http.MethodPost, "/api/schedule", body)
	switch {
	case asked == nil && status >= 300:
		return scheduleRefusal("add", status, reply)
	case asked == nil:
		if err := json.Unmarshal(reply, &sc); err != nil || sc.ID == "" {
			return fmt.Errorf("schedule add: the daemon's reply could not be read; check the schedule list before trying again")
		}
		sc.RunAt = runAt
	default:
		store, release, err := offlineScheduleStore(cfg, asked)
		if err != nil {
			return err
		}
		defer release()
		added, err := store.AddFull(session.AddOptions{
			SessionID: sessionID, SessionName: sessionName, ScheduleName: scheduleName,
			Command: command, RunAt: runAt, CronExpr: cronExpr,
		})
		if err != nil {
			return fmt.Errorf("schedule add: %w", err)
		}
		sc = *added
		fmt.Println("It fires once datawatch is running.")
	}
	when := "on next input prompt"
	if !sc.RunAt.IsZero() {
		when = sc.RunAt.Format("2006-01-02 15:04")
	}
	ref := sessionID
	if sessionName != "" {
		ref = fmt.Sprintf("%s (name:%s)", sessionID, sessionName)
	}
	label := sc.ID
	if scheduleName != "" {
		label = fmt.Sprintf("%s (%s)", sc.ID, scheduleName)
	}
	fmt.Printf("Scheduled [%s] for session %s at %s:\n  %s\n", label, ref, when, command)
	return nil
}

func runScheduleList(cfg *config.Config) error {
	status, reply, asked := scheduleRequest(cfg, http.MethodGet, "/api/schedules", nil)
	if asked == nil {
		if status >= 300 {
			return scheduleRefusal("list", status, reply)
		}
		var entries []*session.ScheduledCommand
		if err := json.Unmarshal(reply, &entries); err != nil {
			return fmt.Errorf("schedule list: read the daemon's reply: %w", err)
		}
		return printSchedules(entries)
	}
	store, release, err := offlineScheduleStore(cfg, asked)
	if err != nil {
		return err
	}
	defer release()
	return printSchedules(store.List())
}

func printSchedules(entries []*session.ScheduledCommand) error {
	if len(entries) == 0 {
		fmt.Println("No scheduled commands.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tTYPE\tSESSION/TARGET\tSCHED-NAME\tSTATE\tWHEN\tCRON\tFIRES\tLAST-FIRE\tLAST-RESULT\tREARM\tTASK/COMMAND")
	for _, sc := range entries {
		when := "on input"
		if !sc.RunAt.IsZero() {
			when = sc.RunAt.Local().Format("01-02 15:04")
		}
		typ := sc.Type
		if typ == "" {
			typ = "command"
		}
		target := sc.SessionID
		if sc.SessionName != "" {
			target = "name:" + sc.SessionName
		}
		if (sc.Type == session.SchedTypeNewSession || sc.Type == session.SchedTypeSpawn) && sc.DeferredSession != nil {
			target = "spawn:" + sc.DeferredSession.Name
		}
		fires := ""
		lastFire := ""
		lastResult := ""
		if sc.FireCount > 0 {
			fires = fmt.Sprintf("%d", sc.FireCount)
			lastFire = sc.LastFireAt.Local().Format("01-02 15:04")
			lastResult = sc.LastFireResult
		}
		rearm := sc.RearmState
		if rearm != "" && sc.LastError != "" {
			rearm += ": " + truncate(sc.LastError, 40)
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			sc.ID, typ, target, sc.ScheduleName,
			sc.State, when, sc.CronExpr,
			fires, lastFire, lastResult, rearm,
			truncate(sc.Command, 40))
	}
	return w.Flush()
}

func runScheduleCancel(cfg *config.Config, id string) error {
	status, reply, asked := scheduleRequest(cfg, http.MethodDelete, "/api/schedule?id="+url.QueryEscape(id), nil)
	if asked != nil {
		store, release, err := offlineScheduleStore(cfg, asked)
		if err != nil {
			return err
		}
		defer release()
		if err := store.Cancel(id); err == nil {
			fmt.Printf("Scheduled command %s cancelled.\n", id)
			return nil
		}
		if store.CancelByScheduleName(id) == 0 {
			return fmt.Errorf("schedule %q not found (tried id and name)", id)
		}
		fmt.Printf("Cancelled schedule(s) named %q.\n", id)
		return nil
	}
	if status == http.StatusNotFound {
		// Try cancel by name
		status, _, asked = scheduleRequest(cfg, http.MethodDelete, "/api/schedule?name="+url.QueryEscape(id), nil)
		if asked != nil {
			return fmt.Errorf("cancel schedule by name: %w", asked)
		}
		if status >= 300 {
			return fmt.Errorf("schedule %q not found (tried id and name)", id)
		}
		fmt.Printf("Cancelled schedule(s) named %q.\n", id)
		return nil
	}
	if status >= 300 {
		return scheduleRefusal("cancel", status, reply)
	}
	fmt.Printf("Scheduled command %s cancelled.\n", id)
	return nil
}
