//Package proxypool provides programmatic access to a pool of proxies
package proxypool

import (
	"encoding/base64"
	"net/url"
)

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

func checkValidScheme(proxyUrl *url.URL) bool {
	switch proxyUrl.Scheme {
	case "http", "https", "socks5":
		return true
	default:
		return false
	}
}
