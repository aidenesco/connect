# proxypool [![GoDoc](https://godoc.org/github.com/aidenesco/proxypool?status.svg)](https://godoc.org/github.com/aidenesco/proxypool) [![Go Report Card](https://goreportcard.com/badge/github.com/aidenesco/proxypool)](https://goreportcard.com/report/github.com/aidenesco/proxypool)
This package distributes requests through a pool of proxies, acting as a proxy gateway that supports CONNECT requests. Proxy rotation spreads out usage as much as possible to avoid IP bans or restrictions. The gateway only accepts proxy urls with the https scheme.


## Installation
```sh
go get -u github.com/aidenesco/proxypool
```

## Usage
Loading proxies
```go
import "github.com/aidenesco/proxypool"

func main() {
    pool := proxypool.NewPool()

    proxyUrl, _ := url.Parse("https://username:password@host:port")

    _ = pool.AddProxy(proxyUrl)
```

Server
```go
import "github.com/aidenesco/proxypool"

func main() {
    pool := proxypool.NewPool()
    //Load pool with your proxies

    server := &http.Server{
        Addr:         ":443",
        Handler:      http.HandlerFunc(pool.Serve),
        TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
    }

    log.Fatal(server.ListenAndServeTLS("your-cert", "your-key"))
}
```