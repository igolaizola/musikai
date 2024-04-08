package proxy

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"time"
)

/*
		 Separate streams across circuits by connection metadata
		 When a stream arrives at Tor, we have the following data to examine:
		 1) The destination address
		 2) The destination port (unless this a DNS lookup)
		 3) The protocol used by the application to send the stream to Tor:
			SOCKS4, SOCKS4A, SOCKS5, or whatever local "transparent proxy"
			mechanism the kernel gives us.
		 4) The port used by the application to send the stream to Tor --
			that is, the SOCKSListenAddress or TransListenAddress that the
			application used, if we have more than one.
		 5) The SOCKS username and password, if any.
		 6) The source address and port for the application.

	   We propose to use 3, 4, and 5 as a back channel for applications to
	   tell Tor about different sessions.  Rather than running only one
	   SOCKSPort, a Tor user who would prefer better session isolation should
	   run multiple SOCKSPorts/TransPorts, and configure different
	   applications to use separate ports. Applications that support SOCKS
	   authentication can further be separated on a single port by their
	   choice of username/password.  Streams sent to separate ports or using
	   different authentication information should never be sent over the
	   same circuit.  We allow each port to have its own settings for
	   isolation based on destination port, destination address, or both.
*/
// TorProxy initializes and returns a TOR SOCKS proxy function for use in a Transport
// TODO: what if we run out of available ports on the machine? create a stream manager possibly...
// TODO: determine if tor socks proxy is running on host machine
func TorProxy() func(*http.Request) (*url.URL, error) {
	// a source of uniformly-distributed pseudo-random
	rand.Seed(time.Now().UnixNano())
	// pseudo-random int value
	num := rand.Intn(0x7fffffff-10000) + 10000
	// base url localhost for now
	base := "socks5://%d:x@127.0.0.1:9050"
	// proxy url with random credentials
	rawUrl := fmt.Sprintf(base, num)
	// parse proxy url
	proxyUrl, err := url.Parse(rawUrl)
	if err != nil {
		fmt.Println("invalid url to parse when creating proxy transport. err: ", err)
		return nil
	}
	// setup proxy transport
	proxy := http.ProxyURL(proxyUrl)

	return proxy
}
