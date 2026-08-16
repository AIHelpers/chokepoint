package proxy

import "net/http"

// HTTPUpstream is the production Upstream implementation: a thin
// wrapper around *http.Client so Gateway can depend on the small
// Upstream interface (and tests can substitute a fake) instead of
// http.Client directly.
type HTTPUpstream struct {
	Client *http.Client
}

// NewHTTPUpstream returns an HTTPUpstream using the given client. If
// client is nil, http.DefaultClient is used.
func NewHTTPUpstream(client *http.Client) *HTTPUpstream {
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPUpstream{Client: client}
}

// Do implements Upstream.
func (h *HTTPUpstream) Do(req *http.Request) (*http.Response, error) {
	return h.Client.Do(req)
}
