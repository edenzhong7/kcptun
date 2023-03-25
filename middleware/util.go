package middleware

import "net"

func blockRead(c net.Conn, p []byte) (n int, err error) {
	offset := 0
	for {
		n, err := c.Read(p[offset:])
		offset += n
		if err != nil {
			return offset, err
		}
		if offset == len(p) {
			break
		}
	}
	return offset, nil
}

func blockWrite(c net.Conn, p []byte) (n int, err error) {
	offset := 0
	for {
		n, err := c.Write(p[offset:])
		offset += n
		if err != nil {
			return offset, err
		}
		if offset == len(p) {
			break
		}
	}
	return offset, nil
}
