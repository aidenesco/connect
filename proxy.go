package proxypool

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"errors"
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

	if p.betweenUse != 0 {
		prev, ok := p.lastUsePerHost[r.Host]
		if ok && time.Since(prev) <= p.betweenUse {
			p.sem.Release(1)
			return false
		}
	}

	p.lastUsePerHost[r.Host] = time.Now()

	p.sem.Release(1)
	return true
}

func (p *proxy) dial(r *http.Request) (conn net.Conn, err error) {
	defer func() {
		if err != nil {
			conn.Close()
		}
	}()

	conn, err = sharedDialer.DialContext(r.Context(), "tcp", p.url.Host)
	if err != nil {
		return
	}

	tconn := tls.Client(conn, &tls.Config{
		ServerName: r.Host,
	})

	err = tconn.Handshake()
	if err != nil {
		return
	}

	conn = tconn

	var connect *http.Request
	connect, err = http.NewRequest(http.MethodConnect, r.Host, nil)
	if err != nil {
		return
	}

	if u := p.url.User; u != nil {
		username := u.Username()
		password, _ := u.Password()
		connect.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+password)))
	}

	err = connect.Write(conn)
	if err != nil {
		return
	}

	bufr := bufio.NewReader(conn)

	var response *http.Response
	response, err = http.ReadResponse(bufr, connect)
	if err != nil {
		return
	}

	switch response.StatusCode {
	case http.StatusOK:
		return
	case http.StatusProxyAuthRequired:
		err = errors.New("invalid or missing \"Proxy-Authorization\" header")
		return
	default:
		err = fmt.Errorf("unexpected CONNECT response status \"%v\" (expect 200 OK)", response.Status)
		return
	}
}
