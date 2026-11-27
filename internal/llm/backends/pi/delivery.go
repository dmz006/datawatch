// The datawatch pi extension: how a pi session fetches its datawatch requests
// and reports that one was in a completed model call (delivery job 4, design
// from the pi fetch proof). The extension is built into datawatch, written to
// the data folder, and loaded with pi's -e by the interactive launch of a wired
// session. A busy pi gets a request after its current task, with a short note
// that the message is real: that pair passed the sandbox proof with the built
// extension and real pi 1.3.0. One-shot pi does not load it.

package pi

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// DeliveryEnv names the variable, in a pi session's environment, holding
	// the path of its delivery credential file. It carries a path, never a
	// token.
	DeliveryEnv = "DATAWATCH_PI_DELIVERY"
	// DeliveryExtensionVersion is what the extension says it is when it
	// reports "seen". Raise it with any change to the extension.
	DeliveryExtensionVersion = "datawatch-pi-delivery-2"
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
	// DeliverAs is DeliverSteer or DeliverFollowUp. datawatch writes follow-up.
	// Empty means the extension fetches nothing.
	DeliverAs string `json:"deliver_as,omitempty"`
	// Guidance adds the short note that tells pi's model these messages are real.
	// datawatch sets it.
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

// interactiveCmd is the interactive launch command of a session wired for
// delivery with extension, or, when it is empty, exactly the command of one
// that is not.
func interactiveCmd(binary, model, thinkingLevel, sessionID, task, projectDir, tmuxSession, agentDir, extension string) (string, error) {
	line, err := buildInteractiveCmdWithAgentDir(binary, model, thinkingLevel, sessionID, task, projectDir, tmuxSession, agentDir)
	if err != nil || extension == "" {
		return line, err
	}
	// The command starts pi with its one trust flag; -e goes right after it.
	start := piInvocation(binary, agentDir) + " --approve "
	if !strings.Contains(line, start) {
		return "", errors.New("pi launch: no place for the delivery extension")
	}
	return strings.Replace(line, start, start+"-e '"+shellEscape(extension)+"' ", 1), nil
}
