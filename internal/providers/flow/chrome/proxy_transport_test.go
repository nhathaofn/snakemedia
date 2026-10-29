package chrome

import (
	"net/url"
	"testing"
)

func TestFlowClientsShareTheProxyTransport(t *testing.T) {
	p1, _ := url.Parse("http://127.0.0.1:9001")
	p2, _ := url.Parse("http://127.0.0.1:9001")
	p3, _ := url.Parse("http://127.0.0.1:9002")
	if proxyTransport(p1) != proxyTransport(p2) {
		t.Fatal("one transport per proxy URL")
	}
	if proxyTransport(p1) == proxyTransport(p3) {
		t.Fatal("different proxies get different transports")
	}
}
