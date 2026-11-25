// Moving and copying reports and moved-aside files without losing content: a
// file is removed only when an identical copy is kept, nothing already on disk
// is replaced, and where a file went is written to the audit log.
package autonomous

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// maxKeptNames bounds the search for a free name.
const maxKeptNames = 1000

// movedFile is one file the daemon moved, or removed as a duplicate. To is
// where its content is now. Identical is true when To already held the same
// content, so the file at From was removed.
type movedFile struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Identical bool   `json:"identical,omitempty"`
}

// moveKeepingBoth moves the file src to target, or, when that name is taken by
// different content or reserved says it must not be used, to the first free
// name <stem>.stray-<n><ext> beside it. A name that already holds the same
// content keeps it, and src is removed. It returns the path that now holds the
// content. Nothing already on disk is ever replaced.
func moveKeepingBoth(src, target string, reserved func(string) bool) (to string, identical bool, err error) {
	ext := filepath.Ext(target)
	stem := strings.TrimSuffix(target, ext)
	if strings.HasSuffix(stem, string(filepath.Separator)) {
		stem, ext = target, "" // a dot file has no extension to keep
	}
	for n := 0; n <= maxKeptNames; n++ {
		cand := target
		if n > 0 {
			cand = stem + ".stray-" + strconv.Itoa(n) + ext
		}
		if reserved != nil && reserved(cand) {
			continue
		}
		merr := moveNoReplace(src, cand)
		if merr == nil {
			return cand, false, nil
		}
		if !errors.Is(merr, fs.ErrExist) {
			return "", false, merr
		}
		// One file reached by two paths is not two copies.
		if filepath.Base(src) == filepath.Base(cand) && sameDirectory(filepath.Dir(src), filepath.Dir(cand)) {
			return "", false, fmt.Errorf("%s and %s are the same file", src, cand)
		}
		if sameFileContent(src, cand) {
			if rerr := os.Remove(src); rerr != nil {
				return "", false, rerr
			}
			return cand, true, nil
		}
	}
	return "", false, fmt.Errorf("no free name for %s beside %s", src, target)
}

// moveNoReplace moves src to dst and fails with fs.ErrExist when anything is
// at dst. os.Rename would replace it. A hard link never replaces; where the
// filesystem has none, the content is copied into an exclusively created file.
func moveNoReplace(src, dst string) error {
	err := os.Link(src, dst)
	if errors.Is(err, fs.ErrExist) {
		return err
	}
	if err != nil {
		if cerr := copyNoReplace(src, dst); cerr != nil {
			return cerr
		}
	}
	return os.Remove(src)
}

// copyNoReplace copies the regular file, or the symbolic link, src to dst. It
// fails with fs.ErrExist when anything is at dst, and refuses any other kind
// of file.
func copyNoReplace(src, dst string) (err error) {
	st, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if st.Mode()&fs.ModeSymlink != 0 {
		link, lerr := os.Readlink(src)
		if lerr != nil {
			return lerr
		}
		return os.Symlink(link, dst)
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}
	in, err := os.Open(src) // #nosec G304 -- a path inside a daemon work tree
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, st.Mode().Perm()) // #nosec G304
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(dst) // only the partial file created above
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// sameFileContent reports whether a and b are both regular files with the same
// bytes. Anything that cannot be read counts as different.
func sameFileContent(a, b string) bool {
	sa, aerr := os.Lstat(a)
	sb, berr := os.Lstat(b)
	if aerr != nil || berr != nil || !sa.Mode().IsRegular() || !sb.Mode().IsRegular() || sa.Size() != sb.Size() {
		return false
	}
	fa, aerr := os.Open(a) // #nosec G304 -- a path inside a daemon work tree
	if aerr != nil {
		return false
	}
	defer func() { _ = fa.Close() }()
	fb, berr := os.Open(b) // #nosec G304 -- a path inside a daemon work tree
	if berr != nil {
		return false
	}
	defer func() { _ = fb.Close() }()
	bufA, bufB := make([]byte, 32*1024), make([]byte, 32*1024)
	for {
		na, ea := io.ReadFull(fa, bufA)
		nb, eb := io.ReadFull(fb, bufB)
		if !bytes.Equal(bufA[:na], bufB[:nb]) {
			return false
		}
		endA := errors.Is(ea, io.EOF) || errors.Is(ea, io.ErrUnexpectedEOF)
		endB := errors.Is(eb, io.EOF) || errors.Is(eb, io.ErrUnexpectedEOF)
		if endA || endB {
			return endA && endB
		}
		if ea != nil || eb != nil {
			return false
		}
	}
}

// auditKeptFiles writes one audit event about files the daemon moved or kept.
// It also logs it, so there is a record when no audit sink is wired.
func (m *Manager) auditKeptFiles(action string, details map[string]any) {
	log.Printf("[autonomous] %s: %v", action, details)
	m.mu.Lock()
	fn := m.auditFn
	m.mu.Unlock()
	if fn != nil {
		fn(action, details)
	}
}
