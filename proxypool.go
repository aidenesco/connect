package proxypool

import (
	"io"
	"net"
)

var sharedDialer net.Dialer

func transfer(destination io.WriteCloser, source io.ReadCloser) {
	defer destination.Close()
	defer source.Close()
	_, _ = io.Copy(destination, source)
}
