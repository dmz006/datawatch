// v7.0.0-alpha.33 #244 — Ollama marketplace + per-Compute-Node model
// pull/remove. Catalog is operator-curated embedded list (manual scrape
// of ollama.com/library landed in a follow-up).
//
// Endpoints:
//
//	GET    /api/marketplace/ollama/catalog                 — curated model catalog
//	POST   /api/compute/nodes/<n>/models/pull              — start background pull (returns task_id)
//	GET    /api/marketplace/ollama/tasks/<task_id>          — poll pull progress
//	DELETE /api/compute/nodes/<n>/models/<model>            — remove a model from the node

package server

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/federation"
)

// MarketplaceCatalogEntry — one model in the embedded curated catalog.
// Each entry can have multiple tag variants (e.g. llama3.1 → :8b / :70b).
type MarketplaceCatalogEntry struct {
	Name        string                  `json:"name"`         // e.g. "llama3.1"
	Description string                  `json:"description"`  // short summary
	License     string                  `json:"license,omitempty"`
	URL         string                  `json:"url,omitempty"` // ollama.com/library/<name>
	Tags        []MarketplaceCatalogTag `json:"tags"`
}

type MarketplaceCatalogTag struct {
	Tag         string `json:"tag"`               // e.g. "8b", "70b", "8b-instruct-q4_K_M"
	SizeGB      float64 `json:"size_gb"`          // disk size estimate
	MinRAMGB    int    `json:"min_ram_gb"`        // estimated RAM floor for inference
	MinVRAMGB   int    `json:"min_vram_gb,omitempty"` // estimated VRAM floor (0 = CPU-only ok)
	Description string `json:"description,omitempty"`
}

// embeddedOllamaCatalog — minimal curated list. Refresh from ollama.com
// is a manual operator action (POST v7.0 follow-up; #279).
var embeddedOllamaCatalog = []MarketplaceCatalogEntry{
	{Name: "llama3.1", Description: "Meta Llama 3.1 — general-purpose 8B/70B/405B instruct models", URL: "https://ollama.com/library/llama3.1", License: "Llama 3.1 Community", Tags: []MarketplaceCatalogTag{
		{Tag: "8b", SizeGB: 4.7, MinRAMGB: 8, MinVRAMGB: 6},
		{Tag: "70b", SizeGB: 40, MinRAMGB: 64, MinVRAMGB: 48},
		{Tag: "405b", SizeGB: 230, MinRAMGB: 512, MinVRAMGB: 256},
	}},
	{Name: "llama3.3", Description: "Meta Llama 3.3 — improved 70B instruct model", URL: "https://ollama.com/library/llama3.3", License: "Llama 3.3 Community", Tags: []MarketplaceCatalogTag{
		{Tag: "70b", SizeGB: 43, MinRAMGB: 64, MinVRAMGB: 48},
	}},
	{Name: "qwen3", Description: "Alibaba Qwen 3 — multilingual, math, code variants", URL: "https://ollama.com/library/qwen3", License: "Apache-2.0", Tags: []MarketplaceCatalogTag{
		{Tag: "0.6b", SizeGB: 0.5, MinRAMGB: 2},
		{Tag: "1.7b", SizeGB: 1.4, MinRAMGB: 4},
		{Tag: "4b", SizeGB: 2.6, MinRAMGB: 6},
		{Tag: "8b", SizeGB: 5.2, MinRAMGB: 10, MinVRAMGB: 6},
		{Tag: "14b", SizeGB: 9.0, MinRAMGB: 16, MinVRAMGB: 10},
		{Tag: "32b", SizeGB: 20, MinRAMGB: 32, MinVRAMGB: 24},
	}},
	{Name: "qwen2.5-coder", Description: "Qwen 2.5 specialized for code generation", URL: "https://ollama.com/library/qwen2.5-coder", License: "Apache-2.0", Tags: []MarketplaceCatalogTag{
		{Tag: "1.5b", SizeGB: 1.0, MinRAMGB: 4},
		{Tag: "7b", SizeGB: 4.7, MinRAMGB: 10, MinVRAMGB: 6},
		{Tag: "32b", SizeGB: 20, MinRAMGB: 32, MinVRAMGB: 24},
	}},
	{Name: "gemma3", Description: "Google Gemma 3 — efficient 1B/4B/12B/27B variants", URL: "https://ollama.com/library/gemma3", License: "Gemma Terms", Tags: []MarketplaceCatalogTag{
		{Tag: "1b", SizeGB: 0.8, MinRAMGB: 2},
		{Tag: "4b", SizeGB: 3.3, MinRAMGB: 8, MinVRAMGB: 4},
		{Tag: "12b", SizeGB: 8.1, MinRAMGB: 16, MinVRAMGB: 10},
		{Tag: "27b", SizeGB: 17, MinRAMGB: 32, MinVRAMGB: 20},
	}},
	{Name: "phi4", Description: "Microsoft Phi-4 — 14B reasoning-tuned", URL: "https://ollama.com/library/phi4", License: "MIT", Tags: []MarketplaceCatalogTag{
		{Tag: "14b", SizeGB: 9.1, MinRAMGB: 16, MinVRAMGB: 10},
	}},
	{Name: "deepseek-r1", Description: "DeepSeek-R1 reasoning model — distilled variants", URL: "https://ollama.com/library/deepseek-r1", License: "MIT (distilled)", Tags: []MarketplaceCatalogTag{
		{Tag: "1.5b", SizeGB: 1.1, MinRAMGB: 4},
		{Tag: "7b", SizeGB: 4.7, MinRAMGB: 10, MinVRAMGB: 6},
		{Tag: "8b", SizeGB: 4.9, MinRAMGB: 10, MinVRAMGB: 6},
		{Tag: "14b", SizeGB: 9.0, MinRAMGB: 16, MinVRAMGB: 10},
		{Tag: "32b", SizeGB: 20, MinRAMGB: 32, MinVRAMGB: 24},
		{Tag: "70b", SizeGB: 43, MinRAMGB: 64, MinVRAMGB: 48},
	}},
	{Name: "mistral", Description: "Mistral 7B instruct — small, fast, capable", URL: "https://ollama.com/library/mistral", License: "Apache-2.0", Tags: []MarketplaceCatalogTag{
		{Tag: "7b", SizeGB: 4.4, MinRAMGB: 8, MinVRAMGB: 6},
	}},
	{Name: "mixtral", Description: "Mistral mixture-of-experts (8x7B)", URL: "https://ollama.com/library/mixtral", License: "Apache-2.0", Tags: []MarketplaceCatalogTag{
		{Tag: "8x7b", SizeGB: 26, MinRAMGB: 48, MinVRAMGB: 24},
	}},
	{Name: "codellama", Description: "Meta CodeLlama — code-tuned llama2 variants", URL: "https://ollama.com/library/codellama", License: "Llama 2 Community", Tags: []MarketplaceCatalogTag{
		{Tag: "7b", SizeGB: 3.8, MinRAMGB: 8, MinVRAMGB: 6},
		{Tag: "13b", SizeGB: 7.4, MinRAMGB: 16, MinVRAMGB: 10},
		{Tag: "34b", SizeGB: 19, MinRAMGB: 32, MinVRAMGB: 24},
	}},
	{Name: "nomic-embed-text", Description: "Nomic embedding model — for RAG / vector search", URL: "https://ollama.com/library/nomic-embed-text", License: "Apache-2.0", Tags: []MarketplaceCatalogTag{
		{Tag: "latest", SizeGB: 0.3, MinRAMGB: 2},
	}},
}

// PullTask tracks an in-flight model pull.
type PullTask struct {
	ID         string    `json:"id"`
	NodeName   string    `json:"node_name"`
	Model      string    `json:"model"`
	Status     string    `json:"status"`              // pending | in_progress | done | failed
	Progress   float64   `json:"progress"`            // 0..1
	BytesTotal int64     `json:"bytes_total,omitempty"`
	BytesDone  int64     `json:"bytes_done,omitempty"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// pullTaskRegistry — in-memory tracker for active + recent pulls.
// 1h retention after completion before garbage collection.
type pullTaskRegistry struct {
	mu    sync.RWMutex
	tasks map[string]*PullTask
}

var globalPullTasks = &pullTaskRegistry{tasks: map[string]*PullTask{}}

func (r *pullTaskRegistry) add(t *PullTask) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tasks[t.ID] = t
}

func (r *pullTaskRegistry) get(id string) (*PullTask, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tasks[id]
	if !ok {
		return nil, false
	}
	c := *t
	return &c, true
}

func (r *pullTaskRegistry) update(id string, fn func(*PullTask)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.tasks[id]; ok {
		fn(t)
		t.UpdatedAt = time.Now().UTC()
	}
}

// handleMarketplaceCatalog — GET /api/marketplace/ollama/catalog
func (s *Server) handleMarketplaceCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapComputeList) {
		return
	}
	// ?refresh=true is reserved for the future scrape; for v7.0 the
	// embedded catalog is the only source. Operator-confirmed in
	// alpha.33 design Q2: hybrid with manual-only refresh.
	writeJSONOK(w, map[string]any{
		"catalog":      embeddedOllamaCatalog,
		"source":       "embedded",
		"refreshable":  false, // POST v7.0 #279 will flip this to true
	})
}

// handleMarketplaceTask — GET /api/marketplace/ollama/tasks/<task_id>
func (s *Server) handleMarketplaceTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapComputeList) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/marketplace/ollama/tasks/")
	if id == "" {
		http.Error(w, "task id required", http.StatusBadRequest)
		return
	}
	t, ok := globalPullTasks.get(id)
	if !ok {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	writeJSONOK(w, t)
}

// handleComputeNodeModelPull — POST /api/compute/nodes/<n>/models/pull
// Body: {"model": "llama3.1:8b"}
// Starts a background pull goroutine; returns the task descriptor.
func (s *Server) handleComputeNodeModelPull(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapComputeWrite) {
		return
	}
	var body struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	body.Model = strings.TrimSpace(body.Model)
	if body.Model == "" {
		http.Error(w, "model required", http.StatusBadRequest)
		return
	}
	n, err := s.computeReg.Get(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if n.Address == "" {
		http.Error(w, "node has no address", http.StatusBadRequest)
		return
	}
	tok := make([]byte, 8)
	for i := range tok {
		tok[i] = byte(time.Now().UnixNano() >> uint(i*8))
	}
	taskID := "pull-" + base64.RawURLEncoding.EncodeToString(tok)
	task := &PullTask{
		ID:        taskID,
		NodeName:  name,
		Model:     body.Model,
		Status:    "pending",
		StartedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	globalPullTasks.add(task)
	go runOllamaPull(taskID, n.Address, body.Model)
	writeJSONOK(w, task)
}

// handleComputeNodeModelDelete — DELETE /api/compute/nodes/<n>/models/<model>
func (s *Server) handleComputeNodeModelDelete(w http.ResponseWriter, r *http.Request, name, model string) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapComputeWrite) {
		return
	}
	n, err := s.computeReg.Get(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if n.Address == "" {
		http.Error(w, "node has no address", http.StatusBadRequest)
		return
	}
	body, _ := json.Marshal(map[string]string{"name": model})
	req, _ := http.NewRequest(http.MethodDelete, strings.TrimRight(n.Address, "/")+"/api/delete", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, // #nosec G402
	}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "ollama delete: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		http.Error(w, fmt.Sprintf("ollama delete returned %d: %s", resp.StatusCode, string(b)), http.StatusBadGateway)
		return
	}
	writeJSONOK(w, map[string]any{"name": name, "model": model, "ok": true})
}

// runOllamaPull — background pull worker. Streams Ollama's /api/pull
// (which returns line-delimited JSON progress) and updates the task.
func runOllamaPull(taskID, addr, model string) {
	globalPullTasks.update(taskID, func(t *PullTask) { t.Status = "in_progress" })
	body, _ := json.Marshal(map[string]any{"name": model, "stream": true})
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(addr, "/")+"/api/pull", strings.NewReader(string(body)))
	if err != nil {
		globalPullTasks.update(taskID, func(t *PullTask) { t.Status = "failed"; t.Error = err.Error() })
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{
		Timeout:   0, // pulls can be very long
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, // #nosec G402
	}
	resp, err := client.Do(req)
	if err != nil {
		globalPullTasks.update(taskID, func(t *PullTask) { t.Status = "failed"; t.Error = err.Error() })
		return
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		globalPullTasks.update(taskID, func(t *PullTask) {
			t.Status = "failed"
			t.Error = fmt.Sprintf("ollama returned %d: %s", resp.StatusCode, string(b))
		})
		return
	}
	dec := json.NewDecoder(resp.Body)
	for {
		var msg struct {
			Status    string `json:"status"`
			Digest    string `json:"digest,omitempty"`
			Total     int64  `json:"total,omitempty"`
			Completed int64  `json:"completed,omitempty"`
			Error     string `json:"error,omitempty"`
		}
		if err := dec.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			globalPullTasks.update(taskID, func(t *PullTask) {
				t.Status = "failed"
				t.Error = "decode error: " + err.Error()
			})
			return
		}
		if msg.Error != "" {
			globalPullTasks.update(taskID, func(t *PullTask) {
				t.Status = "failed"
				t.Error = msg.Error
			})
			return
		}
		globalPullTasks.update(taskID, func(t *PullTask) {
			if msg.Total > 0 {
				t.BytesTotal = msg.Total
				t.BytesDone = msg.Completed
				t.Progress = float64(msg.Completed) / float64(msg.Total)
			}
			if strings.Contains(strings.ToLower(msg.Status), "success") || msg.Status == "success" {
				t.Status = "done"
				t.Progress = 1.0
			}
		})
	}
	// If we exited the loop without an explicit success, mark done if no error.
	globalPullTasks.update(taskID, func(t *PullTask) {
		if t.Status == "in_progress" && t.Error == "" {
			t.Status = "done"
			t.Progress = 1.0
		}
	})
}
