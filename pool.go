package proxypool

import (
	"container/ring"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

//Pool holds a ring of proxies used to distribute requests
type Pool struct {
	amu     sync.Mutex
	pool    *ring.Ring
	perHost map[string]*ring.Ring
}

//NewPool returns a new, empty pool
func NewPool() *Pool {
	return &Pool{perHost: make(map[string]*ring.Ring)}
}

func (p *Pool) addProxy(proxy *proxy) error {
	newRing := ring.New(1)
	newRing.Value = proxy

	p.amu.Lock()
	defer p.amu.Unlock()

	if p.pool == nil {
		p.pool = newRing
	} else {
		p.pool = p.pool.Link(newRing)
	}
	return nil
}

//AddProxy adds a proxy to the pool, with options applied
func (p *Pool) AddProxy(proxyUrl string, options ...ProxyOption) error {
	parsedUrl, err := url.Parse(proxyUrl)
	if err != nil {
		return err
	}

	if !checkValidScheme(parsedUrl) {
		return errors.New("proxypool: invalid proxy url scheme, must be one of [\"http\", \"https\", \"socks5\"]")
	}

	proxy := &proxy{
		url:        parsedUrl,
		betweenUse: 0,
		times:      make(map[string]time.Time),
	}

	for _, v := range options {
		v(proxy)
	}

	return p.addProxy(proxy)
}

func (p *Pool) getConn(ctx context.Context, r *http.Request) (net.Conn, error) {
	p.amu.Lock()
	startAt, ok := p.perHost[r.URL.Host]
	if !ok {
		startAt = p.pool
		p.perHost[r.URL.Host] = startAt
	} else {
		p.perHost[r.URL.Host] = startAt.Next()
	}
	p.amu.Unlock()

	connChan := make(chan net.Conn, 1)

	go func() {
		current := startAt.Prev()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				current = current.Next()
				p, ok := current.Value.(*proxy)
				if !ok {
					return
				}

				if !p.canServe(r) {
					continue
				}

				conn, err := p.dial(r.URL.String())
				if err != nil {
					continue
				}

				connChan <- conn
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
		return nil, errors.New("proxypool: context done before establishing connection")
	case conn := <-connChan:
		return conn, nil
	}

}

//Serve is an http handler to serve as a gateway proxy. Only accepts CONNECT requests
func (p *Pool) Serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodConnect {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	pConn, err := p.getConn(r.Context(), r)
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	cConn, _, err := hijacker.Hijack()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	go func(destination io.WriteCloser, source io.ReadCloser) {
		defer destination.Close()
		defer source.Close()

		_, _ = io.Copy(destination, source)
	}(pConn, cConn)
	go func(destination io.WriteCloser, source io.ReadCloser) {
		defer destination.Close()
		defer source.Close()
		_, _ = io.Copy(destination, source)
	}(cConn, pConn)
}

//Proxy is a function to use as http.Transport.Proxy
func (p *Pool) Proxy(r *http.Request) (*url.URL, error) {
	p.amu.Lock()
	startAt, ok := p.perHost[r.URL.Host]
	if !ok {
		startAt = p.pool
		p.perHost[r.URL.Host] = startAt
	} else {
		p.perHost[r.URL.Host] = startAt.Next()
	}
	p.amu.Unlock()

	current := startAt.Prev()

	for {
		select {
		case <-r.Context().Done():
			return nil, errors.New("proxypool: context done before proxy was found")
		default:
			current = current.Next()
			p, ok := current.Value.(*proxy)
			if !ok {
				return nil, errors.New("proxypool: invalid proxy cast")
			}

			if !p.canServe(r) {
				continue
			}

			return p.url, nil
		}

	}

}
