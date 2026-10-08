package gate

import (
	"testing"
	"time"
)

func TestRunTimeoutReturnsOutput(t *testing.T) {
	out, err := runTimeout(5*time.Second, "sh", "-c", "echo hi")
	if err != nil || out != "hi\n" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestRunTimeoutKillsASlowChildAndItsPipe(t *testing.T) {
	start := time.Now()
	_, err := runTimeout(200*time.Millisecond, "sh", "-c", "sleep 30 & sleep 30")
	if err == nil || err.Error() != "timeout" {
		t.Fatalf("want timeout, got %v", err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("took %s", took)
	}
}

func TestRunTimeoutReportsAFailingCommand(t *testing.T) {
	if _, err := runTimeout(5*time.Second, "sh", "-c", "exit 3"); err == nil || err.Error() == "timeout" {
		t.Fatalf("got %v", err)
	}
}
