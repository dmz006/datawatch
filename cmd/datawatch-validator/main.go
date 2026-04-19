// BL103 — validator agent entrypoint.
//
// Built into the validator container image and run as the entrypoint
// when the parent spawns a validator-profile worker. Reads its
// target worker from DATAWATCH_VALIDATE_TARGET_AGENT_ID, runs every
// check, and reports the verdict back to the parent.
//
// Build:
//   go build -o bin/datawatch-validator ./cmd/datawatch-validator
//
// Container:
//   docker build -t registry.example.com/datawatch/validator:vX.Y.Z \
//     -f images/validator/Dockerfile .

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/validator"
)

func main() {
	cfg := validator.Config{
		ParentURL: firstNonEmpty(
			os.Getenv("DATAWATCH_BOOTSTRAP_URL"),
			os.Getenv("DATAWATCH_PARENT_URL"),
		),
		Token:    os.Getenv("DATAWATCH_TOKEN"),
		WorkerID: os.Getenv("DATAWATCH_VALIDATE_TARGET_AGENT_ID"),
	}
	if cfg.WorkerID == "" {
		// Fallback: in early integrations the orchestrator may set
		// DATAWATCH_TASK to the worker ID. Try to recover.
		if t := os.Getenv("DATAWATCH_TASK"); strings.HasPrefix(t, "validate session ") {
			parts := strings.Fields(t)
			for i, w := range parts {
				if w == "agent" && i+1 < len(parts) {
					cfg.WorkerID = parts[i+1]
				}
			}
		}
	}
	if cfg.ParentURL == "" || cfg.WorkerID == "" {
		fmt.Fprintln(os.Stderr,
			"validator: missing DATAWATCH_BOOTSTRAP_URL or DATAWATCH_VALIDATE_TARGET_AGENT_ID")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := validator.Validate(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "validator: validate: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("validator verdict: %s (%d reason(s))\n", res.Verdict, len(res.Reasons))
	for _, r := range res.Reasons {
		fmt.Printf("  - %s\n", r)
	}

	if myID := os.Getenv("DATAWATCH_AGENT_ID"); myID != "" {
		if err := res.Report(ctx, cfg, myID); err != nil {
			fmt.Fprintf(os.Stderr, "validator: report: %v\n", err)
			os.Exit(1)
		}
	}
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
