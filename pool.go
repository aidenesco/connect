package proxypool

import (
	"container/ring"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/sync/semaphore"
)

//ProxyOption is an option that modifies a Proxy
type ProxyOption func(*proxy)

//WithBetweenUse sets a duration for a proxy to wait before it's used again on the same host
func WithBetweenUse(dur time.Duration) ProxyOption {
	return func(proxy *proxy) {
		proxy.betweenUse = dur
	}
}

//Pool holds a ring of proxies used to distribute requests
type Pool struct {
	sem     *semaphore.Weighted
	ring    *ring.Ring
	perHost map[string]*ring.Ring
}

//NewPool returns a new, empty ring
func NewPool() *Pool {
	return &Pool{
		perHost: make(map[string]*ring.Ring),
		sem:     semaphore.NewWeighted(1),
	}
}

func (p *Pool) addProxy(ctx context.Context, proxy *proxy) error {
	newRing := ring.New(1)
	newRing.Value = proxy

	p.sem.Acquire(ctx, 1)

	if p.ring == nil {
		p.ring = newRing
	} else {
		p.ring = p.ring.Link(newRing)
	}

	p.sem.Release(1)

	return nil
}

//AddProxy adds a proxy to the ring, with options applied
func (p *Pool) AddProxy(url *url.URL, options ...ProxyOption) error {
	if !checkValidScheme(url) {
		return errors.New("proxypool: invalid proxy url scheme, must be one of [\"http\", \"https\", \"socks5\"]")
	}

	proxy := &proxy{
		url:            url,
		betweenUse:     0,
		lastUsePerHost: make(map[string]time.Time),
	}

	for _, v := range options {
		v(proxy)
	}

	return p.addProxy(context.Background(), proxy)
}

func (p *Pool) getNextProxy(r *http.Request) (*proxy, error) {
	err := p.sem.Acquire(r.Context(), 1)
	if err != nil {
		return nil, err
	}

	current, ok := p.perHost[r.Host]
	if !ok {
		current = p.ring
		p.perHost[r.Host] = current
	} else {
		p.perHost[r.Host] = current.Next()
	}

	p.sem.Release(1)

	proxy, ok := current.Value.(*proxy)
	if !ok {
		return nil, errors.New("bad type")
	}

	if !proxy.canServe(r) {
		return nil, errors.New("unable to serve this request")
	}
	return proxy, nil
}

func (p *Pool) getConn(r *http.Request) (net.Conn, error) {
	for {
		select {
		case <-r.Context().Done():
			return nil, errors.New("proxypool: context done before establishing connection")
		default:
			proxy, err := p.getNextProxy(r)
			if err != nil {
				continue
			}

			conn, err := proxy.dial(r)
			if err != nil {
				continue
			}
			return conn, err
		}
	}
}

//Serve is an http handler to serve as a gateway proxy. Only accepts CONNECT requests
func (p *Pool) Serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodConnect {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	pConn, err := p.getConn(r)
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
	for {
		select {
		case <-r.Context().Done():
			return nil, errors.New("proxypool: context done before proxy was found")
		default:
			proxy, err := p.getNextProxy(r)
			if err != nil {
				continue
			}
			return proxy.url, nil
		}
	}
}
