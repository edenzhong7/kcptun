package middleware

import (
	"log"
	"net"
)

type ConnMiddleware interface {
	WrapClient(conn net.Conn) (net.Conn, error)
	WrapServer(conn net.Conn) (net.Conn, error)
}

type wrappedListener struct {
	net.Listener
	mws []ConnMiddleware
}

func (wl *wrappedListener) Accept() (net.Conn, error) {
	conn, err := wl.Listener.Accept()
	if err != nil {
		return nil, err
	}
	log.Printf("new server conn from %s to %s", conn.RemoteAddr().String(), conn.LocalAddr().String())
	for _, mw := range wl.mws {
		conn, err = mw.WrapServer(conn)
		if err != nil {
			log.Printf("wrap server conn failed, from %s to %s, err: %v", conn.RemoteAddr().String(), conn.LocalAddr().String(), err)
			return nil, err
		}
	}
	log.Printf("wrapped server conn  from %s to %s", conn.RemoteAddr().String(), conn.LocalAddr().String())

	return conn, nil
}

func WrapClientConn(conn net.Conn, mws ...ConnMiddleware) (net.Conn, error) {
	var err error
	for _, mw := range mws {
		conn, err = mw.WrapClient(conn)
		if err != nil {
			return nil, err
		}
	}
	return conn, nil
}

func WrapServerListener(lis net.Listener, mws ...ConnMiddleware) net.Listener {
	return &wrappedListener{
		Listener: lis,
		mws:      mws,
	}
}
