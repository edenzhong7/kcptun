package middleware

import (
	"errors"
	"log"
	"net"
	"time"

	"github.com/cybozu-go/netutil"
)

type Conn interface {
	net.Conn
	netutil.HalfCloser
}

type ConnMiddleware interface {
	Name() string
	WrapClient(conn net.Conn) (net.Conn, error)
	WrapServer(conn net.Conn) (net.Conn, error)
}

type wrappedListener struct {
	net.Listener
	mws []ConnMiddleware
}

func newLazyServerConn(conn net.Conn, mws []ConnMiddleware) net.Conn {
	c := &lazyConn{
		Conn:   conn,
		mws:    mws,
		ready:  make(chan struct{}),
		closed: make(chan struct{}),
	}
	go c.init()
	return c
}

type lazyConn struct {
	net.Conn
	mws    []ConnMiddleware
	ready  chan struct{}
	closed chan struct{}
}

func (l *lazyConn) init() {
	conn := l.Conn
	mws := l.mws

	log.Printf("new server conn from %s to %s", conn.RemoteAddr().String(), conn.LocalAddr().String())
	var err error
	for _, mw := range mws {
		conn, err = mw.WrapServer(conn)
		log.Printf("apply mw %s", mw.Name())
		if err != nil {
			log.Printf("[%s] wrap server conn failed, err: %v", mw.Name(), err)
			return
		}
	}
	l.Conn = conn
	log.Printf("wrapped server conn  from %s to %s", conn.RemoteAddr().String(), conn.LocalAddr().String())
	close(l.ready)
}

func (l *lazyConn) Close() error {
	err := l.Conn.Close()
	close(l.closed)
	return err
}

func (l *lazyConn) Read(p []byte) (int, error) {
	select {
	case <-l.ready:
		// pass
	case <-l.closed:
		return 0, errors.New("conn closed")
	case <-time.NewTimer(time.Second * 5).C:
		return 0, errors.New("conn mw setup timeout")
	}
	return l.Conn.Read(p)
}

func (l *lazyConn) Write(p []byte) (int, error) {
	select {
	case <-l.ready:
		// pass
	case <-l.closed:
		return 0, errors.New("conn closed")
	case <-time.NewTimer(time.Second * 5).C:
		return 0, errors.New("conn mw setup timeout")
	}
	return l.Conn.Write(p)
}

func (wl *wrappedListener) Accept() (net.Conn, error) {
	conn, err := wl.Listener.Accept()
	if err != nil {
		return nil, err
	}

	return newLazyServerConn(conn, wl.mws), nil

	// log.Printf("new server conn from %s to %s", conn.RemoteAddr().String(), conn.LocalAddr().String())
	// for _, mw := range wl.mws {
	// 	conn, err = mw.WrapServer(conn)
	// 	log.Printf("apply mw %s", mw.Name())
	// 	if err != nil {
	// 		log.Printf("[%s] wrap server conn failed, err: %v", mw.Name(), err)
	// 		return nil, err
	// 	}
	// }

	// log.Printf("wrapped server conn  from %s to %s", conn.RemoteAddr().String(), conn.LocalAddr().String())
	// return conn, nil
}

func WrapClientConn(conn net.Conn, mws ...ConnMiddleware) (net.Conn, error) {
	log.Printf("new client conn from %s to %s", conn.LocalAddr().String(), conn.RemoteAddr().String())

	var err error
	for _, mw := range mws {
		log.Printf("apply mw %s", mw.Name())
		conn, err = mw.WrapClient(conn)
		if err != nil {
			log.Printf("[%s] wrap client conn failed, err: %v", mw.Name(), err)
			return nil, err
		}
	}
	log.Printf("wrapped client conn from %s to %s", conn.LocalAddr().String(), conn.RemoteAddr().String())
	return conn, nil
}

func WrapServerListener(lis net.Listener, mws ...ConnMiddleware) net.Listener {
	return &wrappedListener{
		Listener: lis,
		mws:      mws,
	}
}
