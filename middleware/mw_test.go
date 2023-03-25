package middleware

import (
	"bufio"
	"fmt"
	"io"
	"math/rand"
	"net"
	"testing"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

var mws []ConnMiddleware

func init() {
	svrConfig, err := loadSvrCert("certs/ca.crt", "certs/server.crt", "certs/server.key")
	if err != nil {
		panic(err)
	}
	cliConfig, err := loadCliCert("certs/ca.crt", "certs/client.crt", "certs/client.key")
	if err != nil {
		panic(err)
	}

	mws = append(mws, compMW{}, randMW{}, tlsWrapper{
		cliConfig: cliConfig,
		svrConfig: svrConfig,
	})
}

func TestXor(t *testing.T) {
	a := 111
	b := 122
	a ^= b
	println(a)
	a ^= b
	println(a)
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
	for {
		conn, err := listen.Accept() // 监听客户端的连接请求
		if err != nil {
			fmt.Println("Accept() failed, err: ", err)
			continue
		}
		conn, err = ApplyServerConn(conn, mws...)
		if err != nil {
			panic("wrap server conn failed: " + err.Error())
		}
		go processSvcConn(conn) // 启动一个goroutine来处理客户端的连接请求
	}
}

func TestRand(t *testing.T) {
	go startTcpServer()
	time.Sleep(time.Second)
	conn, err := net.Dial("tcp", "127.0.0.1:9999")
	if err != nil {
		fmt.Println("err : ", err)
		return
	}
	conn, err = ApplyClientConn(conn, mws...)
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
