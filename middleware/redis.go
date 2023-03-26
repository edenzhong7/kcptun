package middleware

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/redcon"
)

func NewRedisMW() ConnMiddleware {
	return redisMW{}
}

type redisMW struct{}

func (rmw redisMW) Name() string {
	return "redis"
}

func (rmw redisMW) WrapClient(conn net.Conn) (net.Conn, error) {
	// rc := &RedisClient{
	// 	conn: conn,
	// }
	rc := &RedisClientV2{
		conn: conn,
		r:    bufio.NewReader(conn),
	}

	err := rc.Ping()
	if err != nil {
		log.Printf("ping redis failed, err: %v", err)
		return nil, err
	}
	c := &redisCliConn{
		Conn:    conn,
		rConn:   rc,
		readBuf: make([]byte, 0, 10240),
	}
	return c, nil
}

func (rmw redisMW) WrapServer(conn net.Conn) (net.Conn, error) {
	c := &redisSvrConn{
		Conn:     conn,
		r:        redcon.NewReader(conn),
		w:        redcon.NewWriter(conn),
		rmu:      sync.Mutex{},
		readBuf:  make([]byte, 0, 10240),
		wmu:      sync.Mutex{},
		writeBuf: make([]byte, 0, 10240),
		closed:   false,
	}
	go c.loop()
	return c, nil
}

type redisSvrConn struct {
	net.Conn
	r *redcon.Reader
	w *redcon.Writer

	rmu     sync.Mutex
	readBuf []byte

	wmu      sync.Mutex
	writeBuf []byte

	closed bool
}

func (rc *redisSvrConn) loop() {
	var (
		cmd redcon.Command
		err error
	)
	for !rc.closed {
		cmd, err = rc.r.ReadCommand()
		if err != nil {
			return
		}
		var args []string
		for _, x := range cmd.Args {
			args = append(args, string(x))
		}
		log.Printf("recv cmd: %v", args)

		switch strings.ToLower(string(cmd.Args[0])) {
		case "ping":
			rc.w.WriteString("PONG")
			err = rc.w.Flush()
			if err != nil {
				log.Printf("write ping resp failed, err: %v", err)
				return
			}
			log.Printf("ack PING")
			continue
		case "quit":
			log.Printf("server conn quit")
			rc.closed = true
			log.Printf("ack QUIT")
			return
		case "set":
			if len(cmd.Args) != 3 {
				log.Printf("invalid set command")
				return
			}
			rc.rmu.Lock()
			sDec, err := base64.StdEncoding.DecodeString(string(cmd.Args[2]))
			if err != nil {
				rc.rmu.Unlock()
				log.Printf("decode set arg failed, err: %v", err)
				return
			}
			rc.readBuf = append(rc.readBuf, sDec...)

			rc.w.WriteString("OK")
			err = rc.w.Flush()
			if err != nil {
				log.Printf("write quit resp failed, err: %v", err)
				rc.rmu.Unlock()
				return
			}
			rc.rmu.Unlock()
			log.Printf("ack SET %d", len(sDec))
		case "get":
			if len(cmd.Args) != 2 {
				log.Printf("invalid get command")
				return
			}
			retry := 5
			for retry > 0 {
				if len(rc.writeBuf) == 0 {
					time.Sleep(time.Millisecond * 2)
					retry--
					continue
				}
				break
			}

			if len(rc.writeBuf) == 0 {
				rc.w.WriteError("many read with no data")
				return
			}

			rc.wmu.Lock()
			bufLen := len(rc.writeBuf)
			sEnc := base64.StdEncoding.EncodeToString(rc.writeBuf)
			rc.w.WriteString(sEnc)
			rc.writeBuf = rc.writeBuf[:0]
			err = rc.w.Flush()
			if err != nil {
				log.Printf("write get resp failed, err: %v", err)
				rc.wmu.Unlock()
				return
			}
			rc.wmu.Unlock()
			log.Printf("ack GET %d", bufLen)
		}
	}
}

func (rc *redisSvrConn) Read(p []byte) (n int, err error) {
	defer func() {
		if n == 0 {
			println("gg")
		}
	}()

	for !rc.closed {
		rc.rmu.Lock()
		if len(rc.readBuf) == 0 {
			rc.rmu.Unlock()
			time.Sleep(time.Millisecond * 5)
			continue
		}

		if len(rc.readBuf) >= len(p)-n {
			copied := copy(p[n:], rc.readBuf)
			rc.readBuf = rc.readBuf[copied:]
			n += copied
			rc.rmu.Unlock()
			return n, nil
		}

		copied := copy(p[n:], rc.readBuf)
		n += copied
		rc.readBuf = rc.readBuf[copied:]
		rc.rmu.Unlock()

		if n == len(p) {
			break
		}
	}

	if rc.closed {
		err = io.EOF
	}

	return n, err
}

func (rc *redisSvrConn) Write(p []byte) (n int, err error) {
	rc.wmu.Lock()
	defer rc.wmu.Unlock()
	rc.writeBuf = append(rc.writeBuf, p...)
	return len(p), nil
}

func (rc *redisSvrConn) Close() error {
	rc.closed = true
	return rc.Conn.Close()
}

type redisCliConn struct {
	net.Conn
	rConn *RedisClientV2

	readBuf []byte
}

func (rc *redisCliConn) Read(p []byte) (n int, err error) {
	defer func() {
		if n == 0 {
			println("gg")
		}
	}()
	if len(rc.readBuf) > len(p) {
		copy(p, rc.readBuf)
		rc.readBuf = rc.readBuf[len(p):]
		n = len(p)
		return n, err
	}

	key := RandString(5)
	for n < len(p) {
		block, err := rc.rConn.Get(key)
		if err != nil {
			return n, err
		}
		sDec, _ := base64.StdEncoding.DecodeString(string(block))
		rc.readBuf = append(rc.readBuf, sDec...)
		wrote := copy(p[n:], rc.readBuf)
		n += wrote
		rc.readBuf = rc.readBuf[wrote:]
	}

	return n, nil
}

func (rc *redisCliConn) Write(p []byte) (n int, err error) {
	key := RandString(5)
	sEnc := base64.StdEncoding.EncodeToString(p)
	err = rc.rConn.Set(key, sEnc)
	return len(p), err
}

func (rc *redisCliConn) Close() error {
	_ = rc.rConn.Quit()
	return rc.Conn.Close()
}

type RedisClientV2 struct {
	conn net.Conn
	r    *bufio.Reader
}

func NewRedisClientV2(address string) (*RedisClientV2, error) {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return nil, err
	}
	return &RedisClientV2{
		conn: conn,
		r:    bufio.NewReader(conn),
	}, nil
}

func (c *RedisClientV2) Ping() error {
	_, err := c.conn.Write([]byte("PING\r\n"))
	if err != nil {
		return err
	}
	resp, err := c.readResponse()
	if err != nil && err != io.EOF {
		return err
	}
	if resp != "PONG" {
		return fmt.Errorf("PING resp is %s, not `PONG`", resp)
	}
	return nil
}

func (c *RedisClientV2) Set(key string, value string) error {
	cmd := fmt.Sprintf("SET %s %s\r\n", key, value)
	_, err := c.conn.Write([]byte(cmd))
	if err != nil {
		return err
	}
	resp, err := c.readResponse()
	if err != nil {
		return err
	}
	if resp != "OK" {
		return fmt.Errorf("SET resp is %s, not `OK`", resp)
	}
	return nil
}

func (c *RedisClientV2) Get(key string) (string, error) {
	cmd := fmt.Sprintf("GET %s\r\n", key)
	_, err := c.conn.Write([]byte(cmd))
	if err != nil {
		return "", err
	}
	return c.readResponse()
}

func (c *RedisClientV2) Quit() error {
	_, err := c.conn.Write([]byte("QUIT\r\n"))
	if err != nil {
		return err
	}
	return nil

	return c.conn.Close()
}

func (c *RedisClientV2) readResponse() (string, error) {
	response, err := c.r.ReadString('\n')
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
		strData, err := c.r.ReadString('\n')
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
	log.Printf("got resp: %s", response)
	return response, nil
}
