package connect

import (
	"net"
	"net/url"
)

type Connectable interface {
	Connection(*url.URL) (net.Conn, error)
}
