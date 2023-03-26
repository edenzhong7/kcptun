package middleware

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/elazarl/goproxy"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

var mws []ConnMiddleware

func init() {
	//svrConfig, err := LoadSvrCert("certs/ca.crt", "certs/server.crt", "certs/server.key")
	//if err != nil {
	//	panic(err)
	//}
	//cliConfig, err := LoadCliCert("certs/ca.crt", "certs/client.crt", "certs/client.key")
	//if err != nil {
	//	panic(err)
	//}

	mws = append(mws,
		redisMW{},
		randMW{},
		//compMW{},
		//tlsWrapper{
		//	cliConfig: cliConfig,
		//	svrConfig: svrConfig,
		//},
	)
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

func helloServer(w http.ResponseWriter, r *http.Request) {
	log.Printf("recv http req")
	fmt.Fprintf(w, "Hello, %s!", r.URL.Path[1:])
	log.Printf("send http resp")
}

func startHttp() {
	http.HandleFunc("/hello", helloServer)
	http.ListenAndServe(":8000", nil)
}

func startHTTPS() {
	err := http.ListenAndServeTLS(":8001", "testcerts/server.crt", "testcerts/server.key", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
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
		fmt.Println("cli send: ", inputInfo[:n])
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
	go startHttp()
	go startHTTPS()
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
				//conn.SetDeadline(time.Now().Add(time.Second * 5))
				log.Printf("new client conn from %s to %s", conn.LocalAddr().String(), conn.RemoteAddr().String())
				conn, err = WrapClientConn(conn, mws...)
				if err != nil {
					log.Printf("wrap client conn failed, err: %v", err)
					return nil, err
				}
				log.Printf("wrapped client conn  from %s to %s", conn.LocalAddr().String(), conn.RemoteAddr().String())
				return conn, err
			},
		},
	}
	//resp, err := cli.Get("http://127.0.0.1:8000/hello")
	resp, err := cli.Get("https://127.0.0.1:8001/hello")
	//resp, err := cli.Get("https://www.baidu.com")
	if err != nil {
		panic(err)
	}
	body := make([]byte, 10240)
	n, err := resp.Body.Read(body)
	body = body[:n]
	resp.Body.Close()
	if err != nil && err != io.EOF {
		panic(err)
	}
	t.Log(string(body))
}
