package proxypool

import (
	"container/ring"
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/sync/semaphore"
)

//ProxyOption is an option that modifies a Proxy
type ProxyOption func(*proxy) error

//WithBetweenUse sets a duration for a proxy to wait before it's used again on the same host
func WithBetweenUse(dur time.Duration) ProxyOption {
	return func(proxy *proxy) error {
		proxy.betweenUse = dur
		return nil
	}
}

//Pool holds a ring of proxies used to distribute requests
type Pool struct {
	sem     *semaphore.Weighted
	ring    *ring.Ring
	perHost map[string]*ring.Ring
}

//NewPool returns a new, empty pool
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
func (p *Pool) AddProxy(url *url.URL, options ...ProxyOption) (err error) {
	if url.Scheme != "https" {
		err = errors.New("proxy: invalid proxy url scheme")
		return
	}

	proxy := &proxy{
		url:            url,
		lastUsePerHost: make(map[string]time.Time),
	}

	for _, v := range options {
		err = v(proxy)
		if err != nil {
			return
		}
	}

	err = p.addProxy(context.Background(), proxy)
	return
}

func (p *Pool) getNextProxy(r *http.Request) (prox *proxy, err error) {
	err = p.sem.Acquire(r.Context(), 1)
	if err != nil {
		return
	}

	current, ok := p.perHost[r.Host]
	if !ok {
		current = p.ring
		p.perHost[r.Host] = current
	} else {
		p.perHost[r.Host] = current.Next()
	}

	p.sem.Release(1)

	prox, ok = current.Value.(*proxy)
	if !ok {
		err = errors.New("bad type")
	}

	if !prox.canServe(r) {
		return nil, errors.New("unable to serve this request")
	}
	return
}

func (p *Pool) getConn(r *http.Request) (conn net.Conn, err error) {
	c := make(chan net.Conn, 1)
	ctx := r.Context()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				//management in here
				proxy, err := p.getNextProxy(r)
				if err != nil {
					continue
				}

				conn, err := proxy.dial(r)
				if err != nil {
					continue
				}

				c <- conn
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			return
		case conn = <-c:
			return
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

	go transfer(pConn, cConn)
	go transfer(cConn, pConn)
}
