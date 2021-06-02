package connect

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/sync/semaphore"
)

type Proxy struct {
	URL *url.URL
}

func (p *Proxy) Connection(to *url.URL) (conn *tls.Conn, err error) {
	conn, err = tls.Dial("tcp", p.URL.Host, nil)
	if err != nil {
		return
	}

	var connect *http.Request
	connect, err = http.NewRequest(http.MethodConnect, to.Host, nil)
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
		return
	case http.StatusProxyAuthRequired:
		err = errors.New("invalid or missing \"Proxy-Authorization\" header")
		return
	default:
		err = fmt.Errorf("unexpected CONNECT response status \"%v\" (expect 200 OK)", response.Status)
		return
	}
}

type proxyContainer struct {
	sem            *semaphore.Weighted
	proxy          *Proxy
	betweenUse     time.Duration
	lastUsePerHost map[string]time.Time
}

func (p *proxyContainer) canServe(r *http.Request) bool {
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

// func (p *proxyContainer) dial(r *http.Request) (conn *tls.Conn, err error) {
// 	defer func() {
// 		if err != nil {
// 			conn.Close()
// 		}
// 	}()

// 	conn, err = tls.Dial("tcp", p.url.String(), nil)
// 	if err != nil {
// 		return
// 	}

// 	var connect *http.Request
// 	connect, err = http.NewRequest(http.MethodConnect, r.URL.String(), nil)
// 	if err != nil {
// 		return
// 	}

// 	if u := p.url.User; u != nil {
// 		username := u.Username()
// 		password, _ := u.Password()
// 		connect.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+password)))
// 	}

// 	err = connect.Write(conn)
// 	if err != nil {
// 		return
// 	}

// 	bufr := bufio.NewReader(conn)

// 	var response *http.Response
// 	response, err = http.ReadResponse(bufr, connect)
// 	if err != nil {
// 		return
// 	}

// 	switch response.StatusCode {
// 	case http.StatusOK:
// 		return
// 	case http.StatusProxyAuthRequired:
// 		err = errors.New("invalid or missing \"Proxy-Authorization\" header")
// 		return
// 	default:
// 		err = fmt.Errorf("unexpected CONNECT response status \"%v\" (expect 200 OK)", response.Status)
// 		return
// 	}
// }
