package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// signalContext returns a context canceled on SIGINT or SIGTERM, so a command
// part-way through paginating stops promptly.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
