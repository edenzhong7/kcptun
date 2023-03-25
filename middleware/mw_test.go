package middleware

import (
	"bufio"
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/elazarl/goproxy"
	"github.com/xtaci/kcp-go/v5"
	"golang.org/x/crypto/pbkdf2"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

var mws []ConnMiddleware

func init() {
	// svrConfig, err := LoadSvrCert("certs/ca.crt", "certs/server.crt", "certs/server.key")
	// if err != nil {
	// 	panic(err)
	// }
	// cliConfig, err := LoadCliCert("certs/ca.crt", "certs/client.crt", "certs/client.key")
	// if err != nil {
	// 	panic(err)
	// }

	mws = append(mws,
		randMW{},
		//compMW{},
		// tlsWrapper{
		// 	cliConfig: cliConfig,
		// 	svrConfig: svrConfig,
		// },
	)
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func RandString(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

// TCP Server端测试
// 处理函数
func processSvcConn(conn net.Conn) {
	defer conn.Close() // 关闭连接
	for {
		reader := bufio.NewReader(conn)
		var buf [128]byte
		n, err := reader.Read(buf[:]) // 读取数据
		if err != nil {
			if err != io.EOF {
				fmt.Println("read from client failed, err: ", err)
			}
			break
		}
		recvStr := string(buf[:n])
		fmt.Println("svc recv: ", recvStr)

		n, err = conn.Write([]byte(recvStr)) // 发送数据
		if err != nil {
			fmt.Println("wraite to client failed, err: ", err)
			break
		}
		fmt.Println("svr send: ", recvStr[:n])
	}
}

func startTcpServer() {
	listen, err := net.Listen("tcp", "127.0.0.1:9999")
	if err != nil {
		fmt.Println("Listen() failed, err: ", err)
		return
	}
	listen = WrapServerListener(listen, mws...)
	for {
		conn, err := listen.Accept() // 监听客户端的连接请求
		if err != nil {
			fmt.Println("Accept() failed, err: ", err)
			continue
		}
		go processSvcConn(conn) // 启动一个goroutine来处理客户端的连接请求
	}
}

func startHttpProxy() {
	listen, err := net.Listen("tcp", "127.0.0.1:9999")
	if err != nil {
		fmt.Println("Listen() failed, err: ", err)
		return
	}
	listen = WrapServerListener(listen, mws...)
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

func startHttpProxyOverKCP() {
	key := pbkdf2.Key([]byte("demo pass"), []byte("demo salt"), 1024, 32, sha1.New)
	block, _ := kcp.NewAESBlockCrypt(key)
	listener, err := kcp.ListenWithOptions("127.0.0.1:9999", block, 10, 3)
	if err != nil {
		panic(err)
	}

	listen := WrapServerListener(listener, mws...)
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

// handleEcho send back everything it received
func handleEcho(conn net.Conn) {
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			log.Println(err)
			return
		}

		n, err = conn.Write(buf[:n])
		if err != nil {
			log.Println(err)
			return
		}
	}
}

func startKCPServer() {
	key := pbkdf2.Key([]byte("demo pass"), []byte("demo salt"), 1024, 32, sha1.New)
	block, _ := kcp.NewAESBlockCrypt(key)
	if listener, err := kcp.ListenWithOptions("127.0.0.1:9999", block, 10, 3); err == nil {
		lis := WrapServerListener(listener, mws...)
		for {
			s, err := lis.Accept()
			if err != nil {
				log.Fatal(err)
			}
			go handleEcho(s)
		}
	} else {
		log.Fatal(err)
	}
}

func TestTcp(t *testing.T) {
	go startTcpServer()
	time.Sleep(time.Second)
	conn, err := net.Dial("tcp", "127.0.0.1:9999")
	if err != nil {
		fmt.Println("err : ", err)
		return
	}
	conn, err = WrapClientConn(conn, mws...)
	if err != nil {
		panic("wrap cli conn failed: " + err.Error())
	}

	defer conn.Close() // 关闭TCP连接
	for i := 0; i < 10; i++ {
		inputInfo := fmt.Sprintf("%d: %s", i, RandString(10))
		n, err := conn.Write([]byte(inputInfo)) // 发送数据
		if err != nil {
			return
		}
		fmt.Println("cli send: ", string(inputInfo[:n]))
		buf := [512]byte{}
		n, err = conn.Read(buf[:])
		if err != nil {
			if err != io.EOF {
				fmt.Println("recv failed, err:", err)
			}
			return
		}
		fmt.Println("cli recv: ", string(buf[:n]))
	}
}

func TestHttp(t *testing.T) {
	go startHttpProxy()
	time.Sleep(time.Second)

	proxyUrl, err := url.Parse("http://127.0.0.1:9999")
	if err != nil {
		panic(err)
	}
	cli := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyUrl),
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				conn, err := net.Dial(network, addr)
				if err != nil {
					return nil, err
				}
				log.Printf("new client conn from %s to %s", conn.LocalAddr().String(), conn.RemoteAddr().String())
				conn, err = WrapClientConn(conn, mws...)
				if err != nil {
					log.Printf("wrap client conn failed, from %s to %s, err: %v", conn.LocalAddr().String(), conn.RemoteAddr().String(), err)
				}
				log.Printf("wrapped client conn  from %s to %s", conn.LocalAddr().String(), conn.RemoteAddr().String())
				return conn, err
			},
		},
	}
	//resp, err := cli.Get("http://127.0.0.1:8000/hello")
	resp, err := cli.Get("https://www.baidu.com")
	if err != nil {
		panic(err)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	t.Log(string(body))
}

func TestKCP(t *testing.T) {
	go startKCPServer()
	time.Sleep(time.Second)

	key := pbkdf2.Key([]byte("demo pass"), []byte("demo salt"), 1024, 32, sha1.New)
	block, _ := kcp.NewAESBlockCrypt(key)

	// dial to the echo server
	sess, err := kcp.DialWithOptions("127.0.0.1:9999", block, 10, 3)
	if err != nil {
		t.Fatal(err)
	}

	conn, err := WrapClientConn(sess, mws...)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close() // 关闭TCP连接
	for i := 0; i < 10; i++ {
		inputInfo := fmt.Sprintf("%d: %s", i, RandString(10))
		n, err := conn.Write([]byte(inputInfo)) // 发送数据
		if err != nil {
			return
		}
		fmt.Println("cli send: ", string(inputInfo[:n]))
		buf := [512]byte{}
		n, err = conn.Read(buf[:])
		if err != nil {
			if err != io.EOF {
				fmt.Println("recv failed, err:", err)
			}
			return
		}
		fmt.Println("cli recv: ", string(buf[:n]))
	}
}

func TestHttpOverKCP(t *testing.T) {
	go startHttpProxyOverKCP()
	time.Sleep(time.Second)

	key := pbkdf2.Key([]byte("demo pass"), []byte("demo salt"), 1024, 32, sha1.New)
	block, _ := kcp.NewAESBlockCrypt(key)

	proxyUrl, err := url.Parse("http://127.0.0.1:9999")
	if err != nil {
		panic(err)
	}
	cli := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyUrl),
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				// dial to the echo server
				sess, err := kcp.DialWithOptions("127.0.0.1:9999", block, 10, 3)
				if err != nil {
					t.Fatal(err)
				}
				conn, err := WrapClientConn(sess, mws...)
				log.Printf("new client conn from %s to %s", conn.LocalAddr().String(), conn.RemoteAddr().String())

				if err != nil {
					log.Printf("wrap client conn failed, from %s to %s, err: %v", conn.LocalAddr().String(), conn.RemoteAddr().String(), err)
				}
				log.Printf("wrapped client conn  from %s to %s", conn.LocalAddr().String(), conn.RemoteAddr().String())
				return conn, err
			},
		},
	}
	//resp, err := cli.Get("http://127.0.0.1:8000/hello")
	resp, err := cli.Get("https://www.baidu.com")
	if err != nil {
		panic(err)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	t.Log(string(body))
}
