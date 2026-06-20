package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

type Request struct {
	Action string `json:"action"`
}

type Response struct {
	OK             bool   `json:"ok"`
	MacvtapLoaded  bool   `json:"macvtap_loaded"`
	InstanceStatus int    `json:"instance_status"`
	Message        string `json:"message"`
}

// ServeIPC 在 unix socket 上提供服务：每个连接读一行 JSON Request、回一行 JSON Response。
func ServeIPC(socketPath string, handle func(Request) Response) (io.Closer, error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return nil, err
	}
	_ = os.Remove(socketPath) // 清理残留
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // 监听已关闭
			}
			go func(c net.Conn) {
				defer c.Close()
				dec := json.NewDecoder(bufio.NewReader(c))
				var req Request
				if err := dec.Decode(&req); err != nil {
					return
				}
				resp := handle(req)
				_ = json.NewEncoder(c).Encode(resp)
			}(conn)
		}
	}()
	return ln, nil
}

// CallIPC 连接 unix socket，发一条 Request、读一条 Response。
// dial 超时 5s；读写 deadline 120s（覆盖 restart-lightos 最长 ~90s 的等待）。
func CallIPC(socketPath string, req Request) (Response, error) {
	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(120 * time.Second))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&resp); err != nil {
		return Response{}, err
	}
	return resp, nil
}
