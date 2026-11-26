package mergepipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// treeState is what a working tree holds beyond its commit: every changed,
// staged and untracked file (ignored files aside) and the content of the
// changes to tracked ones. listing is empty for a clean tree.
type treeState struct{ listing, content string }

func (m *Merger) treeState(ctx context.Context, dir string) (treeState, error) {
	listing, err := m.gitOK(ctx, dir, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return treeState{}, err
	}
	diff, err := m.gitOK(ctx, dir, "diff", "--no-ext-diff", "--no-textconv", "--binary", "HEAD")
	if err != nil {
		return treeState{}, err
	}
	sum := sha256.Sum256([]byte(diff))
	return treeState{listing: strings.TrimSpace(listing), content: hex.EncodeToString(sum[:])}, nil
}

// files is the listing, cut short for a step's detail.
func (t treeState) files() string {
	lines := strings.Split(t.listing, "\n")
	if len(lines) > 10 {
		lines = append(lines[:10], "...")
	}
	return strings.Join(lines, ", ")
}
