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

	"github.com/thinkgos/go-socks5/ccsocks5"
)

type Proxy struct {
	URL *url.URL
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

		connect := &http.Request{
			Method: http.MethodConnect,
			URL:    &url.URL{Opaque: to.Host},
			Host:   to.Host,
			Header: make(http.Header),
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
