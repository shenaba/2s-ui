package network

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// firstReadTimeout bounds the peek that decides between a plaintext HTTP
// request and a TLS handshake. Without it a peer that connects and then sends
// nothing pins a goroutine and a file descriptor for the life of the process.
//
// A var so the test can shrink it: the deadline is the whole mechanism here, and
// the alternative is a unit test that sits for twenty seconds.
var firstReadTimeout = 20 * time.Second

type AutoHttpsConn struct {
	net.Conn

	firstBuf []byte
	firstErr error
	bufStart int

	readRequestOnce sync.Once
}

func NewAutoHttpsConn(conn net.Conn) net.Conn {
	return &AutoHttpsConn{
		Conn: conn,
	}
}

func (c *AutoHttpsConn) readRequest() bool {
	buf := make([]byte, 2048)
	_ = c.Conn.SetReadDeadline(time.Now().Add(firstReadTimeout))
	n, err := c.Conn.Read(buf)
	// Cleared unconditionally: past this point the connection belongs to the
	// TLS handshake, which sets its own deadlines.
	_ = c.Conn.SetReadDeadline(time.Time{})

	if n > 0 {
		c.firstBuf = buf[:n]
	}
	if err != nil {
		// Held rather than dropped. Reporting it to the caller as a short read
		// let the TLS handshake retry against a connection that was already
		// gone.
		c.firstErr = err
		return false
	}

	reader := bytes.NewReader(c.firstBuf)
	bufReader := bufio.NewReader(reader)
	request, err := http.ReadRequest(bufReader)
	if err != nil {
		return false
	}
	// The version fields have to be filled in. A zero-valued http.Response
	// serialises its status line as "HTTP/0.0 307 Temporary Redirect", which
	// curl rejects outright ("unsupported HTTP version") and browsers treat as
	// a broken connection -- so this redirect from http:// to https:// on the
	// TLS port has never actually worked. Close tells the client not to wait
	// for a second response on a connection that is about to go away.
	resp := http.Response{
		StatusCode: http.StatusTemporaryRedirect,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     http.Header{},
		Close:      true,
	}
	location := fmt.Sprintf("https://%v%v", request.Host, request.RequestURI)
	resp.Header.Set("Location", location)
	_ = resp.Write(c.Conn)
	c.Close()
	c.firstBuf = nil
	return true
}

func (c *AutoHttpsConn) Read(buf []byte) (int, error) {
	c.readRequestOnce.Do(func() {
		c.readRequest()
	})

	if c.firstBuf != nil {
		n := copy(buf, c.firstBuf[c.bufStart:])
		c.bufStart += n
		if c.bufStart >= len(c.firstBuf) {
			c.firstBuf = nil
		}
		return n, nil
	}

	if c.firstErr != nil {
		err := c.firstErr
		c.firstErr = nil
		return 0, err
	}

	return c.Conn.Read(buf)
}
