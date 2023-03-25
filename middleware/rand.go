package middleware

import (
	"errors"
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

var _ ConnMiddleware = randMW{}

type randMW struct{}

func (r randMW) WrapClient(conn net.Conn) (net.Conn, error) {
	passwordLength := rand.Intn(128) + 128
	password := RandPassword(passwordLength)
	n, err := blockWrite(conn, []byte{byte(len(password))})
	if err != nil {
		return nil, err
	}
	if n != 1 {
		return nil, errors.New("write password length failed")
	}
	n, err = blockWrite(conn, []byte(password))
	if err != nil {
		return nil, err
	}
	if n != len(password) {
		return nil, errors.New("write password failed")
	}
	return &shuffleStream{
		conn:        conn,
		readOffset:  0,
		writeOffset: 0,
		pl:          len(password),
		pass:        password,
	}, nil
}

func (r randMW) WrapServer(conn net.Conn) (net.Conn, error) {
	bl := make([]byte, 1)
	n, err := blockRead(conn, bl)
	if err != nil {
		return nil, err
	}
	if n != 1 {
		return nil, errors.New("read password length failed")
	}
	passwordLength := int(bl[0])
	password := make(password, passwordLength)
	n, err = blockRead(conn, password)
	if err != nil {
		return nil, err
	}
	if n != passwordLength {
		return nil, errors.New("read password failed")
	}

	return &shuffleStream{
		conn:        conn,
		readOffset:  0,
		writeOffset: 0,
		pl:          len(password),
		pass:        password,
	}, nil
}

type shuffleStream struct {
	conn        net.Conn
	readOffset  int
	writeOffset int
	pl          int
	pass        password
}

func (c *shuffleStream) Read(p []byte) (n int, err error) {
	n, err = c.conn.Read(p)
	if err != nil {
		println("gg")
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
	n, err = c.conn.Write(pCopy)
	c.writeOffset += n
	return n, err
}

func (c *shuffleStream) Close() error {
	return c.conn.Close()
}

func (c *shuffleStream) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *shuffleStream) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *shuffleStream) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}

func (c *shuffleStream) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

func (c *shuffleStream) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}
