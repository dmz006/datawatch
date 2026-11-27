// Occurrence keys. A key names the occurrence, never the content, so a repeat
// of the same words is a new request and finished work is not redone. Owner:
// part A.
//
// The forms (each part an ID or a number):
//
//	hello:<session>:<binding generation>
//	operator:<random or the caller's own ID>
//	answer:<question>:<answer id>
//	schedule:<schedule id>:<time slot>
//	rules:<role>@<scope>:<change number>
//	wake:<scope>:<condition>:<episode number>
//	bug:<intake record>:<number>
//	exit:<hook>:<session>:<number>
//	reply:<message id>
//	gap:<list id>:<list hash>
//	backlog:<entry key>:<entry episode>
//	reissue:<request id>
//	cond:<condition id>:<number>  (made by Observe)

package delivery

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// keyPartRe is one identifier: no space, so no sentence can be a part.
var keyPartRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@+-]{0,255}$`)

// OccurrenceKey joins a form and its parts into a key. It refuses an empty
// part and a part that is not an identifier.
func OccurrenceKey(form string, parts ...string) (string, error) {
	all := append([]string{form}, parts...)
	if len(parts) == 0 {
		return "", errors.Join(ErrRequestInput, errors.New("an occurrence key needs a form and at least one part"))
	}
	for _, p := range all {
		if !keyPartRe.MatchString(p) {
			return "", errors.Join(ErrRequestInput, fmt.Errorf("an occurrence key part must be an identifier, not %d other characters", len(p)))
		}
	}
	return strings.Join(all, ":"), nil
}

// KeyHash is what the readable index stores.
func KeyHash(key string) string {
	sum := sha256.Sum256([]byte("datawatch request key\x00" + key))
	return hex.EncodeToString(sum[:])
}
