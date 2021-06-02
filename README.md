# connect [![GoDoc](https://godoc.org/github.com/aidenesco/connect?status.svg)](https://godoc.org/github.com/aidenesco/connect) [![Go Report Card](https://goreportcard.com/badge/github.com/aidenesco/connect)](https://goreportcard.com/report/github.com/aidenesco/connect)
This package distributes requests through a pool of proxies, acting as a proxy gateway that supports CONNECT requests. Proxy rotation spreads out usage as much as possible to avoid IP bans or restrictions. The gateway only accepts proxy urls with the https scheme.


## Installation
```sh
go get -u github.com/aidenesco/connect
```

## Usage
Loading proxies
```go
import "github.com/aidenesco/connect"

func main() {
    pool := connect.NewPool()

    proxyUrl, _ := url.Parse("https://username:password@host:port")

    _ = pool.AddProxy(proxyUrl)
```

Server
```go
import "github.com/aidenesco/connect"

func main() {
    pool := connect.NewPool()
    //Load pool with your proxies

    server := &http.Server{
        Addr:         ":443",
        Handler:      http.HandlerFunc(pool.Serve),
        TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
    }

    log.Fatal(server.ListenAndServeTLS("your-cert", "your-key"))
}
```