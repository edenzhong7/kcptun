package middleware

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"

	"github.com/tidwall/redcon"
)

func NewRedisMWV2() ConnMiddleware {
	return redisMWV2{}
}

type redisMWV2 struct{}

func (r redisMWV2) Name() string {
	return "redis"
}

func (r redisMWV2) WrapClient(conn net.Conn) (net.Conn, error) {
	c := &redisCliConnV2{
		Conn: conn,
		r:    bufio.NewReader(conn),
		rbuf: make([]byte, 10240),
		rc:   make(chan []byte),
		done: make(chan struct{}),
	}
	go c.loop()
	return c, nil
}

func (r redisMWV2) WrapServer(conn net.Conn) (net.Conn, error) {
	c := &redisSvrConnV2{
		Conn: conn,
		r:    redcon.NewReader(conn),
		w:    redcon.NewWriter(conn),
		rc:   make(chan []byte),
		rbuf: make([]byte, 10240),
		done: make(chan struct{}),
	}
	go c.loop()
	return c, nil
}

type redisCliConnV2 struct {
	net.Conn

	r *bufio.Reader

	rbuf []byte
	rc   chan []byte

	done chan struct{}
}

func (rc *redisCliConnV2) loop() {
	for {
		resp, err := rc.readResponse()
		if err != nil {
			log.Printf("cli conn read err: %v", err)
			break
		}
		if len(resp) == 0 {
			log.Printf("cli resp is empty")
			continue
		}
		sDec, err := base64.StdEncoding.DecodeString(resp)
		if err != nil {
			log.Printf("cli decode resp [%s] failed, err: %v", resp, err)
			continue
		}

		select {
		case rc.rc <- sDec:
			log.Printf("cli read: %d", len(sDec))
		case <-rc.done:
			log.Printf("cli is closed")
			return
		}
	}
}

func (rc *redisCliConnV2) readResponse() (string, error) {
	response, err := rc.r.ReadString('\n')
	if err != nil {
		return "", err
	}
	response = strings.TrimRight(response, "\r\n")

	if len(response) == 0 {
		return "", nil
	}

	switch response[0] {
	case '+':
		return strings.TrimSpace(response[1:]), nil
	case '-':
		return "", errors.New(response[1:])
	case '$':
		l, err := strconv.ParseInt(response[1:], 10, 64)
		if err != nil {
			return "", err
		}
		strData, err := rc.r.ReadString('\n')
		if err != nil {
			return "", err
		}
		strData = strings.TrimRight(strData, "\r\n")
		if int(l) != len(strData) {
			return "", fmt.Errorf("string data len doesn't match, want: %d, got: %d", l, len(strData))
		}
		return strData, nil
	}
	response = strings.TrimSpace(response)
	return response, nil
}

func (rc *redisCliConnV2) Read(p []byte) (n int, err error) {
	n = copy(p, rc.rbuf)
	rc.rbuf = rc.rbuf[n:]
	if n == len(p) {
		return n, nil
	}

	select {
	case <-rc.done:
		return n, errors.New("cli conn closed")
	case block := <-rc.rc:
		copied := copy(p[n:], block)
		n += copied
		rc.rbuf = append(rc.rbuf, block[copied:]...)
	}
	return n, nil
}

func (rc *redisCliConnV2) Write(p []byte) (n int, err error) {
	key := RandString(5)
	sEnc := base64.StdEncoding.EncodeToString(p)
	cmd := []byte(fmt.Sprintf("*3\r\n$3\r\nSET\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n", len(key), key, len(sEnc), sEnc))
	for n < len(cmd) {
		wrote, err := rc.Conn.Write(cmd[n:])
		if err != nil {
			log.Printf("cli wrote err: %v", err)
			return n, err
		}
		n += wrote
	}
	return len(p), nil
}

func (rc *redisCliConnV2) Close() error {
	close(rc.done)
	return rc.Conn.Close()
}

type redisSvrConnV2 struct {
	net.Conn
	r *redcon.Reader
	w *redcon.Writer

	rc   chan []byte
	rbuf []byte

	done chan struct{}
}

func (rc *redisSvrConnV2) loop() {
	for {
		cmd, err := rc.r.ReadCommand()
		if err != nil {
			log.Printf("svr conn read err: %v", err)
			break
		}
		if len(cmd.Args) == 3 {
			sDec, _ := base64.StdEncoding.DecodeString(string(cmd.Args[2]))
			select {
			case rc.rc <- sDec:
				log.Printf("svr read: %d", len(sDec))
			case <-rc.done:
				log.Printf("svr is closed")
				return
			}
		} else {
			var arr []string
			for _, x := range cmd.Args {
				arr = append(arr, string(x))
			}
			log.Printf("svr conn, invalid cmd: %v", arr)
		}
	}
}

func (rc *redisSvrConnV2) Read(p []byte) (n int, err error) {
	n = copy(p, rc.rbuf)
	rc.rbuf = rc.rbuf[n:]
	if n == len(p) {
		return n, nil
	}

	select {
	case <-rc.done:
		return n, errors.New("cli conn closed")
	case block := <-rc.rc:
		copied := copy(p[n:], block)
		n += copied
		rc.rbuf = append(rc.rbuf, block[copied:]...)
	}
	return n, nil
}

func (rc *redisSvrConnV2) Write(p []byte) (n int, err error) {
	sEnc := base64.StdEncoding.EncodeToString(p)
	rc.w.WriteBulkString(sEnc)
	err = rc.w.Flush()
	return len(p), err
}

func (rc *redisSvrConnV2) Close() error {
	close(rc.done)
	return rc.Conn.Close()
}
