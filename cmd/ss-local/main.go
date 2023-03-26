package main

import (
	"flag"
	"io"
	"log"
	"math/rand"
	"net"
	"time"

	"github.com/xtaci/kcptun/middleware"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

var mws []middleware.ConnMiddleware

func init() {
	svrConfig, err := middleware.LoadSvrCert("certs/ca.crt", "certs/server.crt", "certs/server.key")
	if err != nil {
		panic(err)
	}
	cliConfig, err := middleware.LoadCliCert("certs/ca.crt", "certs/client.crt", "certs/client.key")
	if err != nil {
		panic(err)
	}

	mws = append(mws,
		middleware.NewRedisMW(),
		middleware.NewRandMW(),
		middleware.NewTlsMW(
			cliConfig,
			svrConfig,
		),
	)
}

func copyConn(src, dst net.Conn) {
	log.Printf("start copy: %s -> %s", src.RemoteAddr().String(), dst.RemoteAddr())
	n, err := io.Copy(dst, src)
	log.Printf("finish copy: %s -> %s, copied: %d, err: %v", src.RemoteAddr().String(), dst.RemoteAddr(), n, err)
}

func forward(srcConn *net.TCPConn, raddr string) {
	defer srcConn.Close()

	dstConn, err := net.Dial("tcp", raddr)
	if err != nil {
		log.Printf("dial ss-server err: %v", err)
		return
	}
	dstConn, err = middleware.WrapClientConn(dstConn, mws...)
	if err != nil {
		log.Printf("wrap cli conn failed, err: %v ", err)
		return
	}
	defer dstConn.Close()

	go copyConn(dstConn, srcConn)
	copyConn(srcConn, dstConn)
	//middleware.Pipe(srcConn, dstConn)
}

func main() {
	laddr := flag.String("addr", "0.0.0.0:10999", "listen addr")
	raddr := flag.String("raddr", "127.0.0.1:9999", "server addr")
	flag.Parse()

	addr, err := net.ResolveTCPAddr("tcp", *laddr)
	if err != nil {
		panic(err)
	}
	lis, err := net.ListenTCP("tcp", addr)
	if err != nil {
		panic(err)
	}
	for {
		conn, err := lis.AcceptTCP()
		if err != nil {
			log.Fatal(err)
		}

		go forward(conn, *raddr)
	}
}
