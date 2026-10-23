//go:build !windows

package cli

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"
)

// The first SIGINT cancels the context instead of killing the process, which
// is what lets a sync stop between stages.
func TestInterruptContextCancelsOnSIGINT(t *testing.T) {
	ctx, stop := InterruptContext(context.Background())
	defer stop()
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("SIGINT did not cancel the context")
	}
}
