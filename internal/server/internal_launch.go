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

// startAdmissionClass decides how a session start is admitted. Only a request
// carrying the daemon's own marker is a worker (already admitted upstream by
// the manager that launched it) or a supervisor; everything else, including a
// request that claims one_shot, is an operator start.
func startAdmissionClass(r *http.Request) capacity.Class {
	v := r.Header.Get(InternalLaunchHeader)
	class, token, ok := strings.Cut(v, ":")
	if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(internalLaunchToken)) != 1 {
		return capacity.ClassOperator
	}
	switch c := capacity.Class(class); c {
	case capacity.ClassWorker, capacity.ClassSupervisor:
		return c
	}
	return capacity.ClassOperator
}
