package connect

import (
	"container/ring"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/sync/semaphore"
)

//ProxyOption is an option that modifies a proxyContainer
type ProxyOption func(*Proxy) error

//WithBetweenUse sets a duration for a proxy to wait before it's used again on the same host
func WithBetweenUse(dur time.Duration) ProxyOption {
	return func(proxy *Proxy) error {
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

//AddProxy adds a Proxy to the pool
func (p *Pool) AddProxy(proxy *Proxy) {
	newRing := ring.New(1)
	newRing.Value = proxy

	p.sem.Acquire(context.Background(), 1)
	defer p.sem.Release(1)

	if p.ring == nil {
		p.ring = newRing
	} else {
		p.ring = p.ring.Link(newRing)
	}
}

func (p *Pool) getNextProxy(r *http.Request) (proxy *Proxy, err error) {
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

	proxy, ok = current.Value.(*Proxy)
	if !ok {
		err = errors.New("bad type")
		return
	}

	if !proxy.canServe(r) {
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

			proxy, err := p.getNextProxy(r)
			if err != nil {
				continue
			}

			conn, err := proxy.Connection(r.URL)
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
		log.Println(err)
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

//Proxy is used to distribute client side requests when using an http.Client
func (p *Pool) Proxy(r *http.Request) (*url.URL, error) {
	proxy, err := p.getNextProxy(r)
	if err != nil {
		return nil, fmt.Errorf("connect: %v", err)
	}

	return proxy.URL, nil
}
