package middleware

import (
	"net"
	"time"

	"github.com/golang/snappy"
	"github.com/pkg/errors"
)

func NewCompMW() ConnMiddleware {
	return compMW{}
}

type compMW struct{}

func (c compMW) WrapClient(conn net.Conn) (net.Conn, error) {
	cs := new(compStream)
	cs.conn = conn
	cs.w = snappy.NewBufferedWriter(conn)
	cs.r = snappy.NewReader(conn)
	return cs, nil
}

func (c compMW) WrapServer(conn net.Conn) (net.Conn, error) {
	return c.WrapClient(conn)
}

type compStream struct {
	conn net.Conn
	w    *snappy.Writer
	r    *snappy.Reader
}

func (c *compStream) Read(p []byte) (n int, err error) {
	return c.r.Read(p)
}

func (c *compStream) Write(p []byte) (n int, err error) {
	if _, err := c.w.Write(p); err != nil {
		return 0, errors.WithStack(err)
	}

	if err := c.w.Flush(); err != nil {
		return 0, errors.WithStack(err)
	}
	return len(p), err
}

func (c *compStream) Close() error {
	return c.conn.Close()
}

func (c *compStream) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *compStream) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *compStream) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}

func (c *compStream) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

func (c *compStream) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}
