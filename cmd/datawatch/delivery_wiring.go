// Wiring for the delivery check: the store key at startup, the sources it
// reads, the message channels it tells the operator on, and the failed filter
// sends nothing else keeps.

package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/delivery"
	pipelinePkg "github.com/dmz006/datawatch/internal/pipeline"
	"github.com/dmz006/datawatch/internal/server"
	"github.com/dmz006/datawatch/internal/session"
)

const (
	// deliveryFirstPassDelay lets the start settle before the first check.
	deliveryFirstPassDelay = 20 * time.Second
	// deliveryRepeatQuiet: a filter that keeps failing is recorded at most
	// this often.
	deliveryRepeatQuiet = 5 * time.Minute
	// deliveryUnlandedTimeout bounds the git reads of the unfinished branches.
	deliveryUnlandedTimeout = 2 * time.Minute
	maxPendingDelivery      = 200
)

// deliveryWiring collects, while the daemon starts, what the check needs.
type deliveryWiring struct {
	keyParams delivery.KeyParams
	key       *delivery.StoreKey

	mu       sync.Mutex
	engine   *delivery.Engine
	channels []delivery.Channel
	// pending are failed sends seen before the engine exists.
	pending  []delivery.Candidate
	recorded map[string]time.Time

	questions *autonomouspkg.QuestionStore
	subs      *autonomouspkg.SubscriptionStore
}

func newDeliveryWiring() *deliveryWiring {
	return &deliveryWiring{keyParams: delivery.DefaultKeyParams, recorded: map[string]time.Time{}}
}

// openKey opens the delivery store key while the password is at hand. It
// writes nothing. An error means the daemon must not start. password is nil
// on a non-secure install.
func (w *deliveryWiring) openKey(cfg *config.Config, password []byte) error {
	k, err := delivery.OpenStoreKey(expandHome(cfg.DataDir), password, w.keyParams)
	if err != nil {
		return err
	}
	w.key = k
	return nil
}

// addChannel registers a message channel. send returns what the backend
// answered.
func (w *deliveryWiring) addChannel(name string, send func(text string) error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	unique := name
	for n := 2; w.hasChannelLocked(unique); n++ {
		unique = fmt.Sprintf("%s#%d", name, n)
	}
	w.channels = append(w.channels, delivery.Channel{Name: unique, Send: send})
}

// addRouterChannel registers a chat channel's own send. imap-mcp with no
// recipient set is left out: its send would go to a made-up address.
func (w *deliveryWiring) addRouterChannel(cfg *config.Config, name string, send func(text string) error) {
	if name == "imap_mcp" && strings.TrimSpace(cfg.ImapMcp.To) == "" {
		return
	}
	w.addChannel(name, send)
}

func (w *deliveryWiring) hasChannelLocked(name string) bool {
	for _, c := range w.channels {
		if c.Name == name {
			return true
		}
	}
	return false
}

func (w *deliveryWiring) channelList() []delivery.Channel {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]delivery.Channel(nil), w.channels...)
}

// filterFailed records that an output filter's typed send did not happen.
// Only the filter, the session and how it failed are kept.
func (w *deliveryWiring) filterFailed(filterID, sessionID, how string) {
	c := delivery.Candidate{Kind: delivery.KindFilter, ID: filterID + "@" + sessionID, State: how,
		Since: time.Now(), Problem: how, Invariant: delivery.InvFailed, Sticky: true, WaitsOn: delivery.WaitsOnOperator}
	key := c.ID + ":" + how
	w.mu.Lock()
	if at, ok := w.recorded[key]; ok && time.Since(at) < deliveryRepeatQuiet {
		w.mu.Unlock()
		return
	}
	w.recorded[key] = c.Since
	e := w.engine
	if e == nil && len(w.pending) < maxPendingDelivery {
		w.pending = append(w.pending, c)
	}
	w.mu.Unlock()
	if e != nil {
		if err := e.Record(c); err != nil {
			log.Printf("[delivery] filter %s session %s: %s not recorded: %v", filterID, sessionID, how, err)
		}
	}
}

// wrapFilterHandlers records the refused and failed sends of input-typing
// output filters. It changes nothing the filters do.
func (w *deliveryWiring) wrapFilterHandlers(h session.ActionHandlers) session.ActionHandlers {
	if mayType := h.MayType; mayType != nil {
		h.MayType = func(sessID, filterID string) error {
			err := mayType(sessID, filterID)
			if err != nil {
				w.filterFailed(filterID, sessID, "refused")
			}
			return err
		}
	}
	if send := h.SendInput; send != nil {
		h.SendInput = func(sessID, filterID, text string) error {
			err := send(sessID, filterID, text)
			if err != nil {
				w.filterFailed(filterID, sessID, "send_failed")
			}
			return err
		}
	}
	if add := h.AddSchedule; add != nil {
		h.AddSchedule = func(sessID, filterID, command string) error {
			err := add(sessID, filterID, command)
			if err != nil {
				w.filterFailed(filterID, sessID, "schedule_failed")
			}
			return err
		}
	}
	return h
}

// deliveryDeps is what the sources read.
type deliveryDeps struct {
	cfg       *config.Config
	sessions  *session.Manager
	schedules *session.ScheduleStore
	autonomy  *autonomouspkg.Manager
	pipelines *pipelinePkg.Executor
	http      *server.HTTPServer
	// firstPassDelay replaces deliveryFirstPassDelay in tests.
	firstPassDelay time.Duration
}

// start runs the check. Call it only once every other store has opened: it is
// the first thing here that writes. It returns nil when the check is off.
func (w *deliveryWiring) start(ctx context.Context, d deliveryDeps) (*delivery.Engine, error) {
	if !d.cfg.Delivery.EnabledOn() {
		fmt.Println("[delivery] the check is off (delivery.enabled: false)")
		return nil, nil
	}
	dataDir := expandHome(d.cfg.DataDir)
	adopt := w.key.IsNew()
	if err := w.key.Commit(); err != nil {
		return nil, err
	}
	dir := filepath.Join(dataDir, delivery.HeartbeatDir)

	var journal *pipelinePkg.Journal
	var journalErr error
	if d.pipelines != nil {
		journal, journalErr = pipelinePkg.NewJournal(filepath.Join(dir, "pipelines.rec"), w.key.Key(), adopt)
		if journalErr != nil {
			log.Printf("[delivery] %v", journalErr)
		} else {
			d.pipelines.SetJournal(journal)
		}
	}

	sources := deliverySources(w, d)
	if d.pipelines != nil {
		sources = append(sources, pipelineSource(journal, journalErr))
	}
	e, err := delivery.New(delivery.Options{
		Dir: dir, Key: w.key.Key(), AdoptPlain: adopt, Host: d.cfg.Hostname,
		Interval: d.cfg.Delivery.Interval(), Sources: sources,
		Sender: delivery.NewChannelSender(w.channelList),
		Notify: func() bool { return d.cfg.Delivery.NotifyOn() },
		Slots:  deliverySlots(d.schedules),
	})
	if err != nil {
		return nil, err
	}
	// Secure mode was just turned on: seal what was plain before anything else.
	if err := e.SealAdopted(); err != nil {
		log.Printf("[delivery] sealing the plain records failed: %v", err)
	}

	w.mu.Lock()
	w.engine = e
	pending := w.pending
	w.pending = nil
	w.mu.Unlock()
	for _, c := range pending {
		if err := e.Record(c); err != nil {
			log.Printf("[delivery] %s not recorded: %v", c.ID, err)
		}
	}
	if d.http != nil {
		d.http.SetDelivery(e)
	}

	delay := d.firstPassDelay
	if delay == 0 {
		delay = deliveryFirstPassDelay
	}
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		last := ""
		e.Run(ctx, func(r delivery.Report, err error) {
			// Logged when it changes. The line holds codes and counts only.
			line := fmt.Sprintf("%s %s: %d new, %d backlog, %d accepted", r.Result, strings.Join(r.Red, ","),
				len(r.Items), len(r.Backlog), len(r.Accepted))
			if err != nil {
				line += ": " + err.Error()
			}
			if line != last {
				log.Printf("[delivery] check (report only): %s", line)
				last = line
			}
		})
	}()
	return e, nil
}
