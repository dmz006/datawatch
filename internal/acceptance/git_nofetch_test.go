//go:build !windows

package acceptance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Reading a project folder never contacts another repository, whatever the
// folder's own settings say about objects it does not have.
func TestReadsFetchNothing(t *testing.T) {
	const absent = "0123456789012345678901234567890123456789"
	ctx := context.Background()
	version := func(sub Subject) { _, _ = Git{}.Version(ctx, sub.Repo, absent) }
	cases := []struct {
		name string
		drop string // an object removed from the folder
		// settings that would record a contact over another transport
		transport func(other, record string) [][2]string
		read      func(sub Subject)
	}{
		{name: "named version", read: version},
		{name: "file id", drop: "HEAD^{tree}", read: func(sub Subject) {
			_, _, _ = Git{}.ObjectID(ctx, sub, "a.txt")
		}},
		{name: "file content", drop: "HEAD:a.txt", read: func(sub Subject) {
			_, _, _ = Git{}.ReadFile(ctx, sub, "a.txt")
		}},
		{name: "clean copy", drop: "HEAD:a.txt", read: func(sub Subject) {
			_ = Git{}.Run(ctx, CheckSpec{Command: "true"}, sub, 20*time.Second)
		}},
		{name: "ssh address", read: version, transport: func(_, record string) [][2]string {
			return [][2]string{{"remote.other.url", "ssh://example.invalid/x"}, {"core.sshCommand", record}, {"protocol.ssh.allow", "always"}}
		}},
		{name: "helper address", read: version, transport: func(_, record string) [][2]string {
			return [][2]string{{"remote.other.url", "ext::sh -c " + strings.ReplaceAll(record, " ", "% ")}, {"protocol.ext.allow", "always"}}
		}},
		{name: "git address", read: version, transport: func(_, record string) [][2]string {
			script := filepath.Join(t.TempDir(), "proxy")
			if err := os.WriteFile(script, []byte("#!/bin/sh\n"+record+"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			return [][2]string{{"remote.other.url", "git://example.invalid/x"}, {"core.gitProxy", script}, {"protocol.git.allow", "always"}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, other := newGitRepo(t), newGitRepo(t)
			other.write("b.txt", "other\n")
			other.commit("other")
			r.write("a.txt", "hello\n")
			head := r.commit("one")
			var gone string
			if tc.drop != "" {
				gone = r.git("rev-parse", tc.drop)
			}
			marker := filepath.Join(t.TempDir(), "contacted")
			record := "touch " + marker + "; false"
			settings := [][2]string{
				{"core.repositoryformatversion", "1"},
				{"extensions.partialClone", "other"},
				{"remote.other.promisor", "true"},
			}
			if tc.transport != nil {
				settings = append(settings, tc.transport(other.dir, record)...)
			} else {
				settings = append(settings, [][2]string{
					{"remote.other.url", other.dir},
					{"remote.other.uploadpack", record},
					{"protocol.file.allow", "always"},
				}...)
			}
			for _, kv := range settings {
				r.git("config", kv[0], kv[1])
			}
			if gone != "" {
				if err := os.Remove(filepath.Join(r.dir, ".git", "objects", gone[:2], gone[2:])); err != nil {
					t.Fatal(err)
				}
			}
			tc.read(Subject{Repo: r.dir, Commit: head})
			if _, err := os.Stat(marker); err == nil {
				t.Error("the read contacted the other repository")
			}
		})
	}
}
