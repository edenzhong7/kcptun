package middleware

import (
	"net"
)

type ConnMiddleware interface {
	WrapClient(conn net.Conn) (net.Conn, error)
	WrapServer(conn net.Conn) (net.Conn, error)
}

func ApplyClientConn(conn net.Conn, mws ...ConnMiddleware) (net.Conn, error) {
	var err error
	for _, mw := range mws {
		conn, err = mw.WrapClient(conn)
		if err != nil {
			return nil, err
		}
	}
	return conn, nil
}

func ApplyServerConn(conn net.Conn, mws ...ConnMiddleware) (net.Conn, error) {
	var err error
	for _, mw := range mws {
		conn, err = mw.WrapServer(conn)
		if err != nil {
			return nil, err
		}
	}
	return conn, nil
}
