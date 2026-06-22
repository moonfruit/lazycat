package main

import (
	"bufio"
	"net"
	"path/filepath"
	"testing"
)

// 起一个 unix echo 服务（模拟 tailscaled.sock），经 tsproxy 的 TCP 监听往返一行，
// 验证哑字节双向转发正确。
func TestForwardRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "echo.sock")

	ul, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ul.Close()
	go func() {
		for {
			c, err := ul.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				line, _ := bufio.NewReader(c).ReadString('\n')
				c.Write([]byte("echo:" + line))
			}(c)
		}
	}()

	tl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tl.Close()
	go serve(tl, sockPath)

	c, err := net.Dial("tcp", tl.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	resp, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if resp != "echo:hello\n" {
		t.Fatalf("got %q, want %q", resp, "echo:hello\n")
	}
}
