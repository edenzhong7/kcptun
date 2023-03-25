.PHONY: all build clean run check cover lint docker help server client

server:
	@go build -o middleware/ss-server github.com/xtaci/kcptun/cmd/ss-server

local:
	@go build -o middleware/ss-local github.com/xtaci/kcptun/cmd/ss-local

all: server local

clean:
	@go clean
	rm --force "xx.out"