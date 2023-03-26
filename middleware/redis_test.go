package middleware

import (
	"fmt"
	"testing"
)

func TestRedis(t *testing.T) {
	client, _ := NewRedisClientV2("localhost:6379")
	defer client.Quit()

	err := client.Ping()
	if err != nil {
		t.Fatal(err)
	}

	err = client.Set("mykey", "myvalue")
	if err != nil {
		t.Fatal(err)
	}

	getResp, err := client.Get("mykey")
	fmt.Println(getResp)
	if err != nil {
		t.Fatal(err)
	}

	err = client.Quit()
	if err != nil {
		t.Fatal(err)
	}
}
