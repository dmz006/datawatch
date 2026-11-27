// The datawatch pi extension: how a pi session fetches its datawatch requests
// and reports that one was in a completed model call (delivery job 4, design
// from the pi fetch proof). The extension is built into datawatch and written
// to the data folder for a launch to load with pi's -e.
//
// No launch command loads it yet. When a busy pi is handed a request (after its
// run, or mid-task), and what the model is told about such messages, are not
// decided; the built extension must also pass the sandbox proof before pi's
// worker row may be relied on.

package pi

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// DeliveryEnv names the variable, in a pi session's environment, holding
	// the path of its delivery credential file. It carries a path, never a
	// token.
	DeliveryEnv = "DATAWATCH_PI_DELIVERY"
	// DeliveryExtensionVersion is what the extension says it is when it
	// reports "seen". Raise it with any change to the extension.
	DeliveryExtensionVersion = "datawatch-pi-delivery-1"
	// DeliverSteer and DeliverFollowUp are pi's two ways to hand a busy agent
	// a message: after its current tool calls, or after its run.
	DeliverSteer    = "steer"
	DeliverFollowUp = "followUp"

	deliveryExtensionFile = "datawatch-delivery.ts"
)

//go:embed delivery_extension.ts
var deliveryExtension []byte

// DeliveryCredential is the private file the extension reads for every call.
type DeliveryCredential struct {
	APIURL     string `json:"api_url"`
	Token      string `json:"token"`
	CACertPath string `json:"ca_cert_path,omitempty"`
	// StateFile keeps the reports not yet recorded: request IDs and digests.
	StateFile string `json:"state_file,omitempty"`
	// DeliverAs is DeliverSteer or DeliverFollowUp. Empty, as datawatch writes
	// it today, means the extension fetches nothing.
	DeliverAs string `json:"deliver_as,omitempty"`
	// Guidance adds the text that tells pi's model what these messages are.
	Guidance bool `json:"guidance,omitempty"`
	// PollMillis overrides how often the extension asks; zero is 2 seconds.
	PollMillis int `json:"poll_ms,omitempty"`
}

// replaceFile writes a file in one step, so a reader never sees half of one.
func replaceFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".dw-pi-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Chmod(mode)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	return err
}

// WriteDeliveryExtension writes the built-in extension into dir, read-only,
// and returns its path. The file is replaced every time, so an edited copy does
// not outlive the next launch.
func WriteDeliveryExtension(dir string) (string, error) {
	path := filepath.Join(dir, deliveryExtensionFile)
	if err := replaceFile(path, deliveryExtension, 0o400); err != nil {
		return "", fmt.Errorf("write the pi delivery extension: %w", err)
	}
	return path, nil
}

// WriteDeliveryCredential writes one session's credential file, readable by
// the owner only.
func WriteDeliveryCredential(path string, c DeliveryCredential) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := replaceFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write the pi delivery credential: %w", err)
	}
	return nil
}
