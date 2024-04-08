package http

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/igolaizola/musikai/pkg/tor/proxy"
)

// SmartProxyClient ...
type SmartProxyClient struct {
	MaxTimeout         time.Duration // Max Timeout
	MaxIdleConnections int           // Max Idle Connections
	once               sync.Once     // sync so we only set up 1 client
	netClient          *http.Client  // http client
}

// New ...
func (s *SmartProxyClient) New() *http.Client {
	s.once.Do(func() {
		// transport configuration
		var netTransport = &http.Transport{
			Proxy:        proxy.SmartProxy(),   // We can use Tor or Smart Proxy - rotating IP addresses - if nil no proxy is used
			MaxIdleConns: s.MaxIdleConnections, // max idle connections
			// Dialer
			Dial: (&net.Dialer{
				Timeout: 20 * time.Second, // max dialer timeout
			}).Dial,
			TLSHandshakeTimeout: 20 * time.Second, // transport layer security max timeout
		}
		// Client
		s.netClient = &http.Client{
			Timeout:   20 * time.Second, // round stripper timeout
			Transport: netTransport,     // how our HTTP requests are made
		}
	})

	return s.netClient
}

// NewIP ...
func (s *SmartProxyClient) NewIP() {

}
