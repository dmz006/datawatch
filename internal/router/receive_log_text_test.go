// The receive log line says that a message arrived, not what it says.

package router

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

func TestReceiveLogCarriesNoMessageText(t *testing.T) {
	sm, err := session.NewManager("h", t.TempDir(), "echo", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sm.Close() })
	r := NewRouter("h", "grp1", &captureBackend{name: "test", capture: func(string) {}}, sm, 20, nil)
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = wr
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(rd)
		done <- string(b)
	}()
	const text = "please run the SECRET-COMMAND now"
	r.HandleTestMessage(text)
	os.Stdout = orig
	_ = wr.Close()
	out := <-done
	if strings.Contains(out, "SECRET-COMMAND") {
		t.Fatalf("the log repeats the message:\n%s", out)
	}
	if !strings.Contains(out, "Received") || !strings.Contains(out, "33 characters") {
		t.Fatalf("the log no longer records that a message arrived:\n%s", out)
	}
}
