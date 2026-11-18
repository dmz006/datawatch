//go:build linux

package shutdowncause

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestReadLineWithTimeout(t *testing.T) {
	t.Run("line arrives", func(t *testing.T) {
		got, err := readLineWithTimeout(strings.NewReader(childStartedLine+"\nrest"), time.Second)
		if err != nil || got != childStartedLine {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("eof without line", func(t *testing.T) {
		if _, err := readLineWithTimeout(strings.NewReader(""), time.Second); err == nil {
			t.Fatal("want error on empty stream")
		}
	})
	t.Run("timeout", func(t *testing.T) {
		r, w := io.Pipe()
		defer func() { _ = w.Close() }()
		if _, err := readLineWithTimeout(r, 20*time.Millisecond); err == nil {
			t.Fatal("want timeout error")
		}
	})
}

func TestWaitForCmdline(t *testing.T) {
	cases := []struct {
		name    string
		seq     []string // successive cmdline reads; "" with errAt means error
		want    string
		wantErr bool
		calls   int
	}{
		{"immediate", []string{"sleep 30"}, "sleep", false, 1},
		{"appears after re-exec", []string{"bash -c x", "bash -c x", "systemctl stop datawatch"}, "datawatch", false, 3}, // service-scope-ok fake cmdline, never executed
		{"never appears", []string{"bash -c x"}, "datawatch", true, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := 0
			read := func(pid int) (string, error) {
				if pid != 42 {
					return "", errors.New("wrong pid")
				}
				i := n
				if i >= len(tc.seq) {
					i = len(tc.seq) - 1
				}
				n++
				return tc.seq[i], nil
			}
			got, err := waitForCmdline(42, tc.want, 100*time.Millisecond, read)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && !strings.Contains(got, tc.want) {
				t.Fatalf("got %q", got)
			}
			if tc.calls > 0 && n != tc.calls {
				t.Fatalf("reads = %d, want %d (early exit)", n, tc.calls)
			}
		})
	}
	t.Run("read errors until deadline", func(t *testing.T) {
		_, err := waitForCmdline(1, "x", 20*time.Millisecond, func(int) (string, error) { return "", errors.New("gone") })
		if err == nil {
			t.Fatal("want error")
		}
	})
}

func TestAwaitChildReadyWith(t *testing.T) {
	okRead := func(int) (string, error) { return "bin kill 7", nil }
	badRead := func(int) (string, error) { return "bin", nil }
	cases := []struct {
		name    string
		stdout  string
		read    func(int) (string, error)
		wantErr bool
	}{
		{"announced and visible", childStartedLine + "\n", okRead, false},
		{"wrong announcement", "hello\n", okRead, true},
		{"no announcement", "", okRead, true},
		{"cmdline never visible", childStartedLine + "\n", badRead, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := awaitChildReadyWith(strings.NewReader(tc.stdout), 9, "kill 7", time.Second, 20*time.Millisecond, tc.read)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
