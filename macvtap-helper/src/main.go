package main

import (
	"flag"
	"log"
)

func main() {
	agent := flag.Bool("agent", false, "run agent mode (host network + netadmin)")
	web := flag.Bool("web", false, "run web frontend mode")
	listen := flag.String("listen", "127.0.0.1:8080", "web listen address (container-local)")
	flag.Parse()

	switch {
	case *agent:
		RunAgent()
	case *web:
		RunWeb(*listen)
	default:
		log.Fatal("specify -agent or -web")
	}
}
