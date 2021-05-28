package proxypool

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/sync/semaphore"
)

type proxy struct {
	sem            semaphore.Weighted
	url            *url.URL
	betweenUse     time.Duration
	lastUsePerHost map[string]time.Time
}

func (p *proxy) canServe(r *http.Request) bool {
	err := p.sem.Acquire(r.Context(), 1)
	if err != nil {
		return false
	}

	prev, ok := p.lastUsePerHost[r.Host]
	if ok && time.Until(prev) <= p.betweenUse {
		return false
	}

	p.lastUsePerHost[r.Host] = time.Now()

	p.sem.Release(1)
	return true
}

func (p *proxy) dial(r *http.Request) (conn net.Conn, err error) {
	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()

	var d net.Dialer

	conn, err = d.DialContext(r.Context(), "tcp", p.url.String())
	if err != nil {
		return nil, fmt.Errorf("proxypool: error dialing the proxy '%v'", err)
	}

	conn = tls.Client(conn, nil)

	connect, err := http.NewRequestWithContext(r.Context(), http.MethodConnect, r.URL.String(), nil)
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
		if response.StatusCode == http.StatusProxyAuthRequired {
			err = fmt.Errorf("proxypool: invalid or missing Proxy-Authentication header")
			return
		}
		err = fmt.Errorf("proxypool: unexpected CONNECT response status '%v' (expect 200 OK)", response.Status)
		return
	}
	return
}
