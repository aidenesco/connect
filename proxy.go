package proxypool

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

//ProxyOption is an option that modifies a Proxy
type ProxyOption func(*proxy)

//WithBetweenUse sets a duration for a proxy to wait before it's used again on the same host
func WithBetweenUse(dur time.Duration) ProxyOption {
	return func(proxy *proxy) {
		proxy.betweenUse = dur
	}
}

type proxy struct {
	mu         sync.Mutex
	url        *url.URL
	betweenUse time.Duration
	times      map[string]time.Time
}

func (p *proxy) checkTime(host string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	previous, ok := p.times[host]
	if ok && previous.Sub(now) <= p.betweenUse {
		return false
	}

	p.times[host] = now
	return true
}

func (p *proxy) canServe(r *http.Request) bool {
	return p.checkTime(r.Host)
}

func (p *proxy) dial(address string) (conn net.Conn, err error) {
	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()

	conn, err = tls.Dial("tcp", p.url.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("proxypool: error dialing the proxy '%v'", err)
	}

	connect, err := http.NewRequest("CONNECT", address, nil)
	if err != nil {
		err = fmt.Errorf("proxypool: error creating the CONNECT request '%v'", err)
		return
	}

	if u := p.url.User; u != nil {
		username := u.Username()
		password, _ := u.Password()
		connect.Header.Set("Proxy-Authorization", "Basic "+basicAuth(username, password))
	}

	if err = connect.Write(conn); err != nil {
		err = fmt.Errorf("proxypool: error sending CONNECT request to proxy '%v'", err)
		return
	}

	bufr := bufio.NewReader(conn)

	response, err := http.ReadResponse(bufr, connect)
	if err != nil {
		err = fmt.Errorf("proxypool: error reading server response to CONNECT request '%v'", err)
		return
	}

	if response.StatusCode != http.StatusOK {
		_ = conn.Close()
		if response.StatusCode == http.StatusProxyAuthRequired {
			err = fmt.Errorf("proxypool: invalid or missing Proxy-Authentication header")
			return
		}
		err = fmt.Errorf("proxypool: unexpected CONNECT response status '%v' (expect 200 OK)", response.Status)
		return
	}
	return
}
