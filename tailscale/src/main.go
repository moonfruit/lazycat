// tsproxy：把 tailscaled 的 LocalAPI unix socket 暴露成 TCP 端口的哑字节转发器。
// 仅做双向 io.Copy，绝不解析/改写 HTTP —— Host/Sec-Tailscale 头由本机 tailscale CLI
// 自带并原样穿过；本进程以 root 连 socket，满足 LocalAPI 的 SO_PEERCRED 校验。
package main

import (
	"flag"
	"io"
	"log"
	"net"
)

func main() {
	listenAddr := flag.String("listen", ":5253", "TCP listen address")
	socketPath := flag.String("socket", "/var/run/tailscale/tailscaled.sock", "tailscaled LocalAPI unix socket")
	flag.Parse()

	ln, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("tsproxy: listen %s: %v", *listenAddr, err)
	}
	log.Printf("tsproxy: forwarding %s -> %s", *listenAddr, *socketPath)
	serve(ln, *socketPath)
}

func serve(ln net.Listener, socketPath string) {
	for {
		c, err := ln.Accept()
		if err != nil {
			log.Printf("tsproxy: accept: %v", err)
			return
		}
		go handle(c, socketPath)
	}
}

func handle(tcpConn net.Conn, socketPath string) {
	defer tcpConn.Close()
	unixConn, err := net.Dial("unix", socketPath)
	if err != nil {
		log.Printf("tsproxy: dial %s: %v", socketPath, err)
		return
	}
	defer unixConn.Close()
	done := make(chan struct{}, 2)
	go func() { io.Copy(unixConn, tcpConn); done <- struct{}{} }()
	go func() { io.Copy(tcpConn, unixConn); done <- struct{}{} }()
	<-done
}
