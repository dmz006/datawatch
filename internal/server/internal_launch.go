package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/capacity"
)

// InternalLaunchHeader marks a session start the daemon itself issued over its
// own loopback (an autonomous worker, a verifier, a judge, a supervisor loop
// job). Its value is "<class>:<token>". The token is random per daemon
// process and never leaves it, so no client can forge the marker; the request
// body (one_shot included) has no say in admission.
const InternalLaunchHeader = "X-Datawatch-Internal-Launch"

var internalLaunchToken = func() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic("internal launch token: " + err.Error())
	}
	return hex.EncodeToString(b)
}()

// InternalLaunchValue is the header value the daemon's own loopback callers
// send for class (capacity.ClassWorker or capacity.ClassSupervisor).
func InternalLaunchValue(class capacity.Class) string {
	return string(class) + ":" + internalLaunchToken
}

// startAdmissionClass decides how a session start is admitted, and, for a
// supervisor credential, which credential did it. Only a request carrying the
// daemon's own marker is a worker (already admitted upstream by the manager that
// launched it) or a supervisor; a request authenticated with a supervisor
// credential is a supervisor too. Everything else, including a request that
// claims one_shot, is an operator start.
func (s *Server) startAdmissionClass(r *http.Request) (capacity.Class, string) {
	v := r.Header.Get(InternalLaunchHeader)
	if class, token, ok := strings.Cut(v, ":"); ok && subtle.ConstantTimeCompare([]byte(token), []byte(internalLaunchToken)) == 1 {
		switch c := capacity.Class(class); c {
		case capacity.ClassWorker, capacity.ClassSupervisor:
			return c, ""
		}
	}
	if name := s.supervisorCredentialName(r); name != "" {
		return capacity.ClassSupervisor, name
	}
	return capacity.ClassOperator, ""
}
