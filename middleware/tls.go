package middleware

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"log"
	"net"
)

var _ ConnMiddleware = tlsWrapper{}

func NewTlsMW(cliCfg, svrCfg *tls.Config) ConnMiddleware {
	return &tlsWrapper{
		cliConfig: cliCfg,
		svrConfig: svrCfg,
	}
}

type tlsWrapper struct {
	cliConfig *tls.Config
	svrConfig *tls.Config
}

func (t tlsWrapper) Name() string {
	return "tls"
}

func (t tlsWrapper) WrapClient(conn net.Conn) (net.Conn, error) {
	ctx := context.Background()
	tlsConn := tls.Client(conn, t.cliConfig)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	return &tlsStream{&tlsConnFork{tlsConn}}, nil
}

func (t tlsWrapper) WrapServer(conn net.Conn) (net.Conn, error) {
	ctx := context.Background()
	tlsConn := tls.Server(conn, t.svrConfig)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	return &tlsStream{&tlsConnFork{tlsConn}}, nil
}

type tlsConnFork struct {
	*tls.Conn
}

func (t *tlsConnFork) CloseRead() error {
	return nil
}

type tlsStream struct {
	net.Conn
}

func (t *tlsStream) Read(p []byte) (n int, err error) {
	n, err = t.Conn.Read(p)
	if err != nil {
		log.Printf("tls read err: %v", err)
	}
	return n, err
}

func (t *tlsStream) Write(p []byte) (n int, err error) {
	n, err = t.Conn.Write(p)
	return n, err
}

func LoadSvrCert(ca, crt, crtKey string) (*tls.Config, error) {
	// 加载服务端证书
	srvCert, err := tls.LoadX509KeyPair(crt, crtKey)
	if err != nil {
		return nil, err
	}

	// 加载 CA，并且添加进 caCertPool，这个 caCertPool 就是用来校验客户端证书的根证书列表
	caCrt, err := ioutil.ReadFile(ca)
	if err != nil {
		return nil, err
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCrt)

	// https tls config
	tlsConfig := &tls.Config{
		//ServerName:         "localhost",
		Certificates:       []tls.Certificate{srvCert},
		ClientCAs:          caCertPool,                     // 专用于校验客户端证书的 CA 池
		ClientAuth:         tls.RequireAndVerifyClientCert, // 校验客户端证书
		InsecureSkipVerify: true,
	}
	tlsConfig.BuildNameToCertificate()
	return tlsConfig, nil
}

func LoadCliCert(ca, crt, crtKey string) (*tls.Config, error) {
	// 加载客户端证书
	cliCert, err := tls.LoadX509KeyPair(crt, crtKey)
	if err != nil {
		return nil, err
	}

	// 加载 CA 根证书
	caCrt, err := ioutil.ReadFile(ca)
	if err != nil {
		return nil, err
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCrt)

	// https tls config
	tlsConfig := &tls.Config{
		//ServerName:         "localhost",
		Certificates:       []tls.Certificate{cliCert},
		RootCAs:            caCertPool, // 校验服务端证书的 CA 池
		InsecureSkipVerify: true,
	}
	tlsConfig.BuildNameToCertificate()
	return tlsConfig, nil
}
