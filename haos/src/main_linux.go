package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"haos-lpk/internal/cli"
	"haos-lpk/internal/oidc"
	"haos-lpk/internal/vm"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("haos: ")
	status, err := cli.Execute(os.Args[1:], startVM, oidc.Run, oidc.Watch)
	if err != nil {
		log.Print(err)
	}
	os.Exit(status)
}

func startVM() (int, error) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	return vm.Run(ctx)
}
