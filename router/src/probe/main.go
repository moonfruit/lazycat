package main

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
)

func main() {
	go func() {
		http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})
		if err := http.ListenAndServe("127.0.0.1:9", nil); err != nil {
			fmt.Printf("health http exited: %v\n", err)
		}
	}()

	pc, err := net.ListenPacket("udp", "0.0.0.0:34")
	if err != nil {
		fmt.Printf("listen failed: %v\n", err)
		os.Exit(-1)
	}
	defer pc.Close()
	fmt.Println("UDP probe listening on :34")
	buf := make([]byte, 64*1024)
	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			fmt.Printf("readfrom error: %v\n", err)
			continue
		}
		dump := hex.EncodeToString(buf[:n])
		if len(dump) > 96 {
			dump = dump[:96] + "..."
		}
		fmt.Printf("from=%s len=%d hex=%s\n", addr, n, dump)
	}
}
