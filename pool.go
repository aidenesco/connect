package connect

import (
	"container/ring"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/sync/semaphore"
)

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

//ProxyOption is an option that modifies a proxyContainer
type ProxyOption func(*proxyContainer) error

//WithBetweenUse sets a duration for a proxy to wait before it's used again on the same host
func WithBetweenUse(dur time.Duration) ProxyOption {
	return func(proxy *proxyContainer) error {
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

func (p *Pool) addProxy(proxyC *proxyContainer) error {
	newRing := ring.New(1)
	newRing.Value = proxyC

	p.sem.Acquire(context.Background(), 1)

	if p.ring == nil {
		p.ring = newRing
	} else {
		p.ring = p.ring.Link(newRing)
	}

	p.sem.Release(1)

	return nil
}

//AddProxy adds a Proxy to the ring, with options applied
func (p *Pool) AddProxy(proxy *Proxy, options ...ProxyOption) (err error) {
	proxyC := &proxyContainer{
		proxy:          proxy,
		lastUsePerHost: make(map[string]time.Time),
		sem:            semaphore.NewWeighted(1),
	}

	for _, o := range options {
		err = o(proxyC)
		if err != nil {
			return
		}
	}

	err = p.addProxy(proxyC)
	return
}

func (p *Pool) getNextProxy(r *http.Request) (proxyC *proxyContainer, err error) {
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

	proxyC, ok = current.Value.(*proxyContainer)
	if !ok {
		err = errors.New("bad type")
		return
	}

	if !proxyC.canServe(r) {
		err = errors.New("unable to serve this request")
		return
	}
	return
}

func (p *Pool) getConn(r *http.Request) (conn net.Conn, err error) {
	c := make(chan net.Conn, 1)
	ctx := r.Context()

	go func() {
		for {
			if ctx.Err() != nil {
				return
			}

			proxyC, err := p.getNextProxy(r)
			if err != nil {
				continue
			}

			conn, err := proxyC.proxy.Connection(r.URL)
			if err != nil {
				continue
			}

			c <- conn
			break
		}
	}()

	select {
	case <-ctx.Done():
		err = ctx.Err()
		return
	case conn = <-c:
		return
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

func (p *Pool) Proxy(r *http.Request) (*url.URL, error) {
	pc, err := p.getNextProxy(r)
	if err != nil {
		return nil, fmt.Errorf("connect: %v", err)
	}

	return pc.proxy.URL, nil
}
