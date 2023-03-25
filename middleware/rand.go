package middleware

import (
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net"
	"time"
)

type password []byte

func init() {
	// 更新随机种子，防止生成一样的随机密码
	rand.Seed(time.Now().Unix())
}

// 产生 256个byte随机组合的 密码，最后会使用base64编码为字符串存储在配置文件中
// 不能出现任何一个重复的byte位，必须又 0-255 组成，并且都需要包含
func RandPassword(passwordLength int) password {
	// 随机生成一个由  0~255 组成的 byte 数组
	intArr := rand.Perm(passwordLength)
	password := make(password, passwordLength)
	for i, v := range intArr {
		password[i] = byte(v)
		if i == v {
			// 确保不会出现如何一个byte位出现重复
			return RandPassword(passwordLength)
		}
	}
	return password
}

func NewRandMW() ConnMiddleware {
	return randMW{}
}

type randMW struct{}

func (r randMW) WrapClient(conn net.Conn) (net.Conn, error) {
	passwordLength := rand.Intn(128) + 128
	password := RandPassword(passwordLength)
	n, err := blockWrite(conn, append([]byte{byte(len(password))}, []byte(password)...))
	if err != nil {
		return nil, err
	}
	if n != len(password)+1 {
		return nil, errors.New("write password failed")
	}
	return &shuffleStream{
		Conn:        conn,
		readOffset:  0,
		writeOffset: 0,
		pl:          len(password),
		pass:        password,
	}, nil
}

func (r randMW) WrapServer(conn net.Conn) (net.Conn, error) {
	bl := make([]byte, 129)
	n, err := blockRead(conn, bl)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, errors.New("read password failed")
	}
	passwordLength := int(bl[0])
	left := passwordLength - (n - 1)
	password := bl[1:]
	if left > 0 {
		buf := make([]byte, left)
		n, err = blockRead(conn, buf)
		if err != nil {
			return nil, err
		}
		if n != left {
			return nil, errors.New("read password failed")
		}
		password = append(password, buf...)
	}

	if len(password) != passwordLength {
		return nil, fmt.Errorf("password len doesn't match, want %d, got: %d", passwordLength, len(password))
	}

	return &shuffleStream{
		Conn:        conn,
		readOffset:  0,
		writeOffset: 0,
		pl:          len(password),
		pass:        password,
	}, nil
}

type shuffleStream struct {
	net.Conn
	readOffset  int
	writeOffset int
	pl          int
	pass        password
}

func (c *shuffleStream) Read(p []byte) (n int, err error) {
	n, err = c.Conn.Read(p)
	if err != nil {
		log.Printf("rand read err: %v", err)
	}
	if err != nil {
		return n, err
	}
	for i := 0; i < n; i++ {
		p[i] = p[i] ^ c.pass[c.readOffset%c.pl]
		c.readOffset++
	}
	return n, nil
}

func (c *shuffleStream) Write(p []byte) (n int, err error) {
	pCopy := make([]byte, len(p))
	copy(pCopy, p)
	for i := 0; i < len(p); i++ {
		pCopy[i] = pCopy[i] ^ c.pass[(c.writeOffset+i)%c.pl]
	}
	n, err = c.Conn.Write(pCopy)
	c.writeOffset += n
	return n, err
}
