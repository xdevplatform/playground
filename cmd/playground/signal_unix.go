// +build unix

package main

import (
	"os"
	"os/signal"
	"syscall"
)

func init() {
	// Override the Unix signal handling
	notifyUNIXSignals = func(sigChan chan<- os.Signal) {
		signal.Notify(sigChan, syscall.SIGTSTP)
	}

	getShutdownMessage = func(sig os.Signal) string {
		if sig == syscall.SIGTSTP {
			return "Received suspend signal (Ctrl+Z), shutting down gracefully..."
		}
		return "Received interrupt signal, shutting down..."
	}
}
