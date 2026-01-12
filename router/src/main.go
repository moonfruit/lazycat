package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"

	"gitee.com/linakesi/remotesocks"
)

func main() {
	port := flag.String("port", "10", "the local listen port")
	target := flag.String("target", "", "the target ip to forward")
	flag.Parse()

	l, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", *port))
	if err != nil {
		fmt.Printf("Listen on %q failed: %v", *port, err)
		os.Exit(-1)
	}

	targetip := *target

	fmt.Printf("TARGET IP IS %v LISTEN PORT IS:%v\n", targetip, *port)

	for {
		conn, err := l.Accept()
		if err != nil {
			panic(err)
		}
		var port uint16
		binary.Read(conn, binary.LittleEndian, &port)

		target := net.JoinHostPort(targetip, strconv.Itoa(int(port)))
		fmt.Printf("BEGIN FORWARD %v -> %v\n", conn.LocalAddr(), target)
		go func() {
			t, err := net.Dial("tcp", target)
			if err != nil {
				fmt.Printf("dial target %v failed, err:%v\n", target, err)
				return
			}
			remotesocks.ForwardConn(context.TODO(), remotesocks.TCPBufSize, t, conn)
		}()
	}
}
