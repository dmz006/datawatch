// Occurrence keys. A key names the occurrence, never the content, so a repeat
// of the same words is a new request and finished work is not redone. Owner:
// part A. STUB.
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

package delivery

// OccurrenceKey joins a form and its parts into a key. It refuses an empty
// part and a part that is not an identifier.
func OccurrenceKey(string, ...string) (string, error) { return "", ErrRequestsNotBuilt }

// KeyHash is what the readable index stores.
func KeyHash(string) string { return "" }
