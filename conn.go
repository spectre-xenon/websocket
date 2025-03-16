package websocket

import (
	"bufio"
	"net"
)

type Conn struct {
	netConn net.Conn

	br *bufio.Reader
	bw *bufio.Writer

	subprotocol string
}

func (c *Conn) readSkip(n int) ([]byte, error) {
	buf, err := c.br.Peek(n)
	c.br.Discard(len(buf))
	return buf, err
}

// Close writes the websocket close frame,
// flushes the buffer and closes the underlying connections.
func (c *Conn) Close() {
	// TODO: write Close frame

	c.bw.Flush()
	c.netConn.Close()
}
