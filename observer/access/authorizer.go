package access

import (
	"errors"
	"net/http"
)

// An authorizer returns one of these to pick the answer: 401 for a caller it could not
// identify, 403 for one it identified and turned away. Any other error is a 403.
var (
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrForbidden       = errors.New("forbidden")
)

var (
	ErrNoSubjectHeader = errors.New("the header naming the caller is not set")
	ErrNoGroupsHeader  = errors.New("there are ceilings per group and no header listing them")
)

// Identity is who the caller is, and the ceiling that comes with them.
type Identity struct {
	// Subject names the caller: a user id, a service account, a token subject.
	Subject string

	// Tenant groups subjects that share a scope. Empty when the deployment has one.
	Tenant string

	// Ceiling narrows the listener's own ceiling for this caller.
	Ceiling Ceiling
}

// Authorizer decides who the caller is. It runs on the web server goroutine, before the
// request reaches an actor, so it must not block for long.
//
// An error is answered with 401 or 403 and no detail. Returning an Identity with a
// narrower Ceiling is how a caller gets less than the listener allows.
type Authorizer interface {
	Authorize(request *http.Request) (Identity, error)
}
