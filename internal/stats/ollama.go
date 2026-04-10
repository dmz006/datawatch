package stats

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OllamaStats holds statistics from a remote Ollama server.
type OllamaStats struct {
	Host           string         `json:"host"`
	Available      bool           `json:"available"`
	LastCheck      time.Time      `json:"last_check"`
	Error          string         `json:"error,omitempty"`
	ModelCount     int            `json:"model_count"`
	TotalSizeBytes int64          `json:"total_size_bytes"`
	RunningModels  []RunningModel `json:"running_models,omitempty"`
	Models         []ModelInfo    `json:"models,omitempty"`
}

// RunningModel is a currently loaded/running model on the Ollama server.
type RunningModel struct {
	Name       string  `json:"name"`
	SizeVRAM   int64   `json:"size_vram"`   // bytes in VRAM
	SizeBytes  int64   `json:"size_bytes"`  // total model size
	ExpiresAt  string  `json:"expires_at,omitempty"`
}

// ModelInfo is a model installed on the Ollama server.
type ModelInfo struct {
	Name       string `json:"name"`
	SizeBytes  int64  `json:"size_bytes"`
	ModifiedAt string `json:"modified_at"`
	Family     string `json:"family,omitempty"`
	Parameters string `json:"parameters,omitempty"`
	Quant      string `json:"quantization,omitempty"`
}

// FetchOllamaStats queries the Ollama API for server statistics.
func FetchOllamaStats(host string) OllamaStats {
	stats := OllamaStats{
		Host:      host,
		LastCheck: time.Now(),
	}
	client := &http.Client{Timeout: 5 * time.Second}

	// Fetch model list
	tagsResp, err := client.Get(host + "/api/tags")
	if err != nil {
		stats.Error = fmt.Sprintf("connect: %v", err)
		return stats
	}
	defer tagsResp.Body.Close()
	if tagsResp.StatusCode != 200 {
		stats.Error = fmt.Sprintf("tags: HTTP %d", tagsResp.StatusCode)
		return stats
	}
	stats.Available = true

	var tagsData struct {
		Models []struct {
			Name       string `json:"name"`
			Size       int64  `json:"size"`
			ModifiedAt string `json:"modified_at"`
			Details    struct {
				Family            string `json:"family"`
				ParameterSize     string `json:"parameter_size"`
				QuantizationLevel string `json:"quantization_level"`
			} `json:"details"`
		} `json:"models"`
	}
	body, _ := io.ReadAll(tagsResp.Body)
	json.Unmarshal(body, &tagsData) //nolint:errcheck

	for _, m := range tagsData.Models {
		stats.TotalSizeBytes += m.Size
		stats.Models = append(stats.Models, ModelInfo{
			Name:       m.Name,
			SizeBytes:  m.Size,
			ModifiedAt: m.ModifiedAt,
			Family:     m.Details.Family,
			Parameters: m.Details.ParameterSize,
			Quant:      m.Details.QuantizationLevel,
		})
	}
	stats.ModelCount = len(stats.Models)

	// Fetch running models
	psResp, err := client.Get(host + "/api/ps")
	if err == nil {
		defer psResp.Body.Close()
		if psResp.StatusCode == 200 {
			var psData struct {
				Models []struct {
					Name      string `json:"name"`
					Size      int64  `json:"size"`
					SizeVRAM  int64  `json:"size_vram"`
					ExpiresAt string `json:"expires_at"`
				} `json:"models"`
			}
			psBody, _ := io.ReadAll(psResp.Body)
			json.Unmarshal(psBody, &psData) //nolint:errcheck
			for _, m := range psData.Models {
				stats.RunningModels = append(stats.RunningModels, RunningModel{
					Name:      m.Name,
					SizeVRAM:  m.SizeVRAM,
					SizeBytes: m.Size,
					ExpiresAt: m.ExpiresAt,
				})
			}
		}
	}

	return stats
}
