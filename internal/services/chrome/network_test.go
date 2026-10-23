package chrome

import (
	"net"
	"testing"
)

func TestRecognizesClosedLoopbackPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	conn, err := net.Dial("tcp", address)
	if conn != nil {
		_ = conn.Close()
		t.Skip("temporary port was reused")
	}
	if !isConnectionRefused(err) {
		t.Fatalf("closed port was not recognized: %v", err)
	}
}
