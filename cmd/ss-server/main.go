package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"time"

	"github.com/elazarl/goproxy"
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

func main() {
	laddr := flag.String("addr", "0.0.0.0:9999", "listen addr")
	flag.Parse()

	listen, err := net.Listen("tcp", *laddr)
	if err != nil {
		fmt.Println("Listen() failed, err: ", err)
		return
	}
	listen = middleware.WrapServerListener(listen, mws...)
	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = true
	proxy.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		fmt.Printf("proxy recv conn from %s to %s, path: %s\n", req.RemoteAddr, req.Host, req.RequestURI)
		return req, nil
	})

	if err = http.Serve(listen, proxy); err != nil {
		log.Fatalf("start proxy failed, err: %v", err)
	}
}
