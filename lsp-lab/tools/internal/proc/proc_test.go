package proc

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"00:05", 5 * time.Second},
		{"1:23.50", 83*time.Second + 500*time.Millisecond},
		{"01:02:03", time.Hour + 2*time.Minute + 3*time.Second},
		{"2-03:00:00", 51 * time.Hour},
	}
	for _, tt := range tests {
		got, err := ParseDuration(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("ParseDuration(%q) = %v, %v; want %v", tt.in, got, err, tt.want)
		}
	}
	if _, err := ParseDuration("x:y"); err == nil {
		t.Error("expected error for garbage input")
	}
}

func TestWorkspaceRoot(t *testing.T) {
	cmd := "/x/server/bin/intellij-server --socket=127.0.0.1:0 --workspace-root /home/dev/proj --idle-timeout 10s"
	if got := WorkspaceRoot(cmd); got != "/home/dev/proj" {
		t.Errorf("WorkspaceRoot = %q", got)
	}
}
