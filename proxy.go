package connect

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

	"github.com/thinkgos/go-socks5/ccsocks5"
	"golang.org/x/sync/semaphore"
)

type Proxy struct {
	URL            *url.URL
	betweenUse     time.Duration
	lastUsePerHost map[string]time.Time
	sem            *semaphore.Weighted
}

func (p *Proxy) Connection(to *url.URL) (conn net.Conn, err error) {
	switch to.Scheme {
	case "https":
		var tConn *tls.Conn
		tConn, err = tls.Dial("tcp", p.URL.Host, nil)

		if err != nil {
			return
		}
		defer func() {
			if err != nil {
				tConn.Close()
			}
		}()

		err = tConn.Handshake()
		if err != nil {
			return
		}

		// connect := &http.Request{
		// 	Method: http.MethodConnect,
		// 	URL:    &url.URL{Opaque: to.Host},
		// 	Host:   to.Host,
		// 	Header: make(http.Header),
		// }

		var connect *http.Request
		connect, err = http.NewRequest(http.MethodConnect, to.String(), nil)
		if err != nil {
			return
		}

		if u := p.URL.User; u != nil {
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
			conn = tConn
			return
		case http.StatusProxyAuthRequired:
			err = errors.New("connect: invalid or missing \"Proxy-Authorization\" header")
			return
		default:
			err = fmt.Errorf("connect: unexpected CONNECT response status \"%s\" (expect 200 OK)", response.Status)
			return
		}
	case "socks5":
		c := ccsocks5.NewClient(p.URL.Host)

		conn, err = c.Dial("tcp", to.Host)
		return
	default:
		err = errors.New("connect: url scheme must be one of: [\"https\", \"socks5\"]")
		return
	}

}

func (p *Proxy) canServe(r *http.Request) bool {
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

func NewProxy(u *url.URL, o ...ProxyOption) (p *Proxy, err error) {
	p = &Proxy{
		URL:            u,
		lastUsePerHost: make(map[string]time.Time),
		sem:            semaphore.NewWeighted(1),
	}

	for _, opt := range o {
		err = opt(p)
		if err != nil {
			return
		}
	}

	return
}
