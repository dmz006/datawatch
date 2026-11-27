// Roles: who holds a role in a scope, and the proof that a session can take.
// Owner: part C. STUB.

package delivery

import "context"

type roleKeeper struct {
	sessions SessionBook
}

// NewRoleKeeper returns the keeper over datawatch's session records.
func NewRoleKeeper(sessions SessionBook) RoleKeeper { return &roleKeeper{sessions: sessions} }

func (k *roleKeeper) Resolve(RequestBook, Role, Scope) (RoleBinding, bool) {
	return RoleBinding{}, false
}

func (k *roleKeeper) Give(context.Context, RequestBook, Caller, RoleInput) (RoleBinding, *CreateInput, error) {
	return RoleBinding{}, nil, ErrRequestsNotBuilt
}

func (k *roleKeeper) Release(context.Context, RequestBook, Caller, RoleInput) error {
	return ErrRequestsNotBuilt
}

func (k *roleKeeper) Ended(context.Context, RequestBook, string) error { return ErrRequestsNotBuilt }

func (k *roleKeeper) Delivered(context.Context, RequestBook, Request) error {
	return ErrRequestsNotBuilt
}
