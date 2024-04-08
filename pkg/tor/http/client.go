package http

import "net/http"

// Client ...
type Client interface {
	New() *http.Client
	NewIP()
}
