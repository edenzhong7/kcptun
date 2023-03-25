package middleware

import (
	"io"
	"net"
	"sync"

	"github.com/cybozu-go/netutil"
)

func blockRead(c net.Conn, p []byte) (n int, err error) {
	offset := 0
	for {
		n, err := c.Read(p[offset:])
		offset += n
		if err != nil {
			return offset, err
		}
		if offset == len(p) {
			break
		}
	}
	return offset, nil
}

func blockWrite(c net.Conn, p []byte) (n int, err error) {
	offset := 0
	for {
		n, err := c.Write(p[offset:])
		offset += n
		if err != nil {
			return offset, err
		}
		if offset == len(p) {
			break
		}
	}
	return offset, nil
}

var pool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 64<<10)
	},
}

func Pipe(srcConn *net.TCPConn, destConn net.Conn) {
	defer destConn.Close()
	wg := &sync.WaitGroup{}

	wg.Add(1)
	go func() error {
		defer func() {
			wg.Done()
		}()

		buf := pool.Get().([]byte)
		_, err := io.CopyBuffer(destConn, srcConn, buf)
		pool.Put(buf)
		if hc, ok := destConn.(netutil.HalfCloser); ok {
			hc.CloseWrite()
		}
		srcConn.CloseRead()
		return err
	}()

	wg.Add(1)
	go func() error {
		defer func() {
			wg.Done()
		}()

		buf := pool.Get().([]byte)
		_, err := io.CopyBuffer(srcConn, destConn, buf)
		pool.Put(buf)
		srcConn.CloseWrite()
		if hc, ok := destConn.(netutil.HalfCloser); ok {
			hc.CloseRead()
		}
		return err
	}()

	wg.Wait()
}
