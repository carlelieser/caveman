package adapters

import "github.com/carlelieser/caveman/internal/ir"

// RequestBody is one provider's wire body, in the key order it arrived in.
type RequestBody = *ir.Object

// Provider is everything the HTTP layer needs to serve one provider. A provider
// owns its route, its wire format, and the shape of its errors, so adding one
// is adding an implementation of this interface rather than editing the
// handler.
type Provider interface {
	Name() string
	// Path is the route this provider serves, and the upstream path requests
	// forward to.
	Path() string
	// BaseURL is where this provider's requests are forwarded, absent an env
	// override.
	BaseURL() string
	ToIR(body RequestBody) ir.Request
	FromIR(request ir.Request) RequestBody
	// ErrorEnvelope wraps a Caveman-generated message in the provider's own
	// error shape.
	ErrorEnvelope(message string) RequestBody
}

// Registry is the set of providers the HTTP layer serves. Registering a second
// provider is appending to this slice; no handler knows any provider by name.
type Registry []Provider

// ByPath finds the provider that claims a route. A route no provider claims is
// not served.
func (r Registry) ByPath(path string) (Provider, bool) {
	for _, provider := range r {
		if provider.Path() == path {
			return provider, true
		}
	}
	return nil, false
}
