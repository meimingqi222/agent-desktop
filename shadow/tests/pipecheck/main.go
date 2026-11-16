package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/meimingqi222/agent-desktop-shadow/internal/ipc"
)

func main() {
	c, err := ipc.Dial("ad-pip-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, "dial:", err)
		os.Exit(1)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := c.Call(ctx, "status", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "call:", err)
		os.Exit(1)
	}
	fmt.Println(string(resp.Payload)[:300])
}
