package provider_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/joeseverino/severino-vault-mcp/internal/provider"
)

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func readPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(path); err == nil && strings.HasSuffix(string(raw), "\n") {
			pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
			if err != nil {
				t.Fatal(err)
			}
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the child never started")
	return 0
}

func TestAHandshakeTimeoutDoesNotLeakTheChild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	spec := provider.Spec{Command: "sh", Args: []string{"-c", `echo $$ > "$0"; exec sleep 60`, pidFile}}
	start := time.Now()
	_, err := provider.Connect(context.Background(), spec, map[string]string{"PATH": os.Getenv("PATH")}, 300*time.Millisecond)
	if err == nil {
		t.Fatal("a silent child completed the handshake")
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("Connect held on to a silent child for %s", took)
	}
	pid := readPID(t, pidFile)
	deadline := time.Now().Add(3 * time.Second)
	for alive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if alive(pid) {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("child %d still running after the handshake timed out", pid)
	}
}

func TestCancellingTheContextEndsTheChild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	spec := provider.Spec{Command: "sh", Args: []string{"-c", `echo $$ > "$0"; exec sleep 60`, pidFile}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := provider.Connect(ctx, spec, map[string]string{"PATH": os.Getenv("PATH")}, time.Minute)
		done <- err
	}()
	pid := readPID(t, pidFile)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		syscall.Kill(pid, syscall.SIGKILL)
		t.Fatal("Connect ignored the cancelled context")
	}
	deadline := time.Now().Add(3 * time.Second)
	for alive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if alive(pid) {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("child %d survived cancellation", pid)
	}
}
