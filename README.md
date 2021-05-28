# proxypool [![GoDoc](https://godoc.org/github.com/aidenesco/proxypool?status.svg)](https://godoc.org/github.com/aidenesco/proxypool) [![Go Report Card](https://goreportcard.com/badge/github.com/aidenesco/proxypool)](https://goreportcard.com/report/github.com/aidenesco/proxypool)
This package distributes requests through a pool of proxies. Proxy rotation spreads out usage as much as possible to avoid IP bans or restrictions. This package can also act as a proxy gateway, accepting CONNECT requests and forwarding them on.


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

    proxyUrl, _ := url.Parse("scheme://username:password@host:port")

    _ = pool.AddProxy(proxyUrl)
```

Client
- can be used with http, https, and socks5 proxies
```go
import "github.com/aidenesco/proxypool"

func main() {
    pool := proxypool.NewPool()
    //Load pool with your proxies

    client := &http.Client{
        Transport: &http.Transport{Proxy: pool.Proxy},
    }
    
    resp, _ := client.Get("https://google.com")
    
    fmt.Println(resp.Status)
}
```

Server
- can be used with https proxies
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