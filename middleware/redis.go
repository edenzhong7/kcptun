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
		rmu:      sync.Mutex{},
		readBuf:  make([]byte, 0, 10240),
		wmu:      sync.Mutex{},
		writeBuf: nil,
		closed:   false,
	}
	go c.loop()
	return c, nil
}

type redisSvrConn struct {
	net.Conn

	rmu     sync.Mutex
	readBuf []byte

	wmu      sync.Mutex
	writeBuf [][]byte

	closed bool
}

func (rc *redisSvrConn) loop() {
	defer func() {
		log.Printf("svc conn loop canceled")
	}()
	reader := bufio.NewReader(rc.Conn)
	writer := bufio.NewWriter(rc.Conn)

	for !rc.closed {
		cmdString, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Error reading:", err.Error())
			return
		}

		cmdString = strings.TrimSuffix(cmdString, "\r\n")
		args := strings.Split(cmdString, " ")
		log.Printf("recv cmd: %v", args)

		switch strings.ToLower(string(args[0])) {
		case "ping":
			_, err = writer.WriteString("+PONG\r\n")
			err = writer.Flush()
			if err != nil {
				log.Printf("write ping resp failed, err: %v", err)
				continue
			}
			log.Printf("ack PING")
		case "quit":
			log.Printf("server conn quit")
			rc.closed = true
			log.Printf("ack QUIT")
			return
		case "set":
			if len(args) != 3 {
				log.Printf("invalid set command")
				continue
			}
			rc.rmu.Lock()
			sDec, err := base64.StdEncoding.DecodeString(args[2])
			if err != nil {
				rc.rmu.Unlock()
				log.Printf("decode set arg failed, err: %v", err)
				continue
			}
			rc.readBuf = append(rc.readBuf, sDec...)

			_, err = writer.WriteString("+OK\r\n")
			err = writer.Flush()

			if err != nil {
				log.Printf("write quit resp failed, err: %v", err)
				rc.rmu.Unlock()
				continue
			}
			rc.rmu.Unlock()
			log.Printf("ack SET %d", len(sDec))
		case "get":
			if len(args) != 2 {
				log.Printf("invalid get command")
				continue
			}
			retry := 5
			for retry > 0 {
				if len(rc.writeBuf) == 0 {
					time.Sleep(time.Millisecond * 10)
					retry--
					log.Printf("write buf is empty, try later")
					continue
				}
				break
			}

			if len(rc.writeBuf) == 0 {
				writer.WriteString("-ERR many read with no data")
				continue
			}

			rc.wmu.Lock()
			bufLen := len(rc.writeBuf[0])
			sEnc := base64.StdEncoding.EncodeToString(rc.writeBuf[0])
			rc.writeBuf = rc.writeBuf[1:]
			writer.WriteString("$" + fmt.Sprintf("%d", len(sEnc)) + "\r\n" + sEnc + "\r\n")
			writer.Flush()
			if err != nil {
				log.Printf("write get resp failed, err: %v", err)
				rc.wmu.Unlock()
				continue
			}
			rc.wmu.Unlock()
			log.Printf("ack GET %d", bufLen)
		}
	}
}

func (rc *redisSvrConn) Read(p []byte) (n int, err error) {
	for !rc.closed {
		if len(rc.readBuf) == 0 {
			time.Sleep(time.Millisecond * 3)
			continue
		}
		break
	}

	rc.rmu.Lock()
	defer rc.rmu.Unlock()

	n = copy(p[n:], rc.readBuf)
	rc.readBuf = rc.readBuf[n:]

	if rc.closed {
		err = io.EOF
	}
	log.Printf("svc conn, want: %d, read: %d", len(p), n)
	return n, err
}

func (rc *redisSvrConn) Write(p []byte) (n int, err error) {
	rc.wmu.Lock()
	defer rc.wmu.Unlock()
	rc.writeBuf = append(rc.writeBuf, append(p[:0:0], p...))
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
	if len(rc.readBuf) > len(p) {
		n = copy(p, rc.readBuf)
		rc.readBuf = rc.readBuf[n:]
		return n, err
	}

	key := RandString(5)
	block, err := rc.rConn.Get(key)
	if err != nil {
		return n, err
	}
	sDec, _ := base64.StdEncoding.DecodeString(block)
	rc.readBuf = append(rc.readBuf, sDec...)
	wrote := copy(p[n:], rc.readBuf)
	n += wrote
	rc.readBuf = rc.readBuf[wrote:]

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
	cmd := []byte(fmt.Sprintf("SET %s %s\r\n", key, value))
	n := 0
	for n < len(cmd) {
		wrote, err := c.conn.Write(cmd[n:])
		if err != nil {
			return err
		}
		n += wrote
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
	return response, nil
}
