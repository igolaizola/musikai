package http

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/igolaizola/musikai/pkg/tor/proxy"
)

// Tor represents an HTTP Client over the Tor network
type Tor struct {
	MaxTimeout         time.Duration   // Max Timeout
	MaxIdleConnections int             // Max Idle Connections
	Transport          *http.Transport // Custom Transport
	once               sync.Once       // sync so we only set up 1 client
	netClient          *http.Client    // http client
}

// New creates an instance of a Tor Client
func (t *Tor) New() *http.Client {
	// ensure that we only create one
	t.once.Do(func() {
		// Transport configuration
		t.Transport = &http.Transport{
			Proxy:        proxy.TorProxy(),     // We can use Tor or Smart Proxy - rotating IP addresses - if nil no proxy is used
			MaxIdleConns: t.MaxIdleConnections, // max idle connections
			// TODO: Change to DialContext because Dial is deprecated
			Dial: (&net.Dialer{
				Timeout: t.MaxTimeout, // max dialer timeout
			}).Dial,
			TLSHandshakeTimeout: t.MaxTimeout, // transport layer security max timeout
		}
		// HTTPClient
		t.netClient = &http.Client{
			Timeout:   t.MaxTimeout, // round tripper timeout
			Transport: t.Transport,  // how our HTTP requests are made
		}
	})

	return t.netClient
}

// NewIP swaps a client's transport with a new one
func (t *Tor) NewIP() {
	// Get a new proxy
	t.Transport.Proxy = proxy.TorProxy()

}
