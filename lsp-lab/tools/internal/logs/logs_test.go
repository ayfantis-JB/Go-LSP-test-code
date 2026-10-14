package logs

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSignatureGroupsRepeats(t *testing.T) {
	a := Signature("2026-10-14 14:19:49,770 [6842769] SEVERE - #c.i.o.a.i.ExceptionsKt - Unhandled exception in [Mode@5e47f201]", 0)
	b := Signature("2026-10-14 14:20:00,348 [6853347] SEVERE - #c.i.o.a.i.ExceptionsKt - Unhandled exception in [Mode@2395458f]", 0)
	if a != b {
		t.Errorf("signatures differ:\n%s\n%s", a, b)
	}
	if !strings.HasPrefix(a, "SEVERE") {
		t.Errorf("timestamp not stripped: %q", a)
	}
}

func TestErrorRegexIgnoresStackFrames(t *testing.T) {
	if errorRe.MatchString("\tat com.intellij.openapi.diagnostic.Logger.error(Logger.java:376)") {
		t.Error("stack frame counted as an error")
	}
	if !errorRe.MatchString("2026-10-14 12:31:36.323 [error] connection lost") {
		t.Error("VS Code [error] line not matched")
	}
}

func TestFollowPrintsNewErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.log")
	os.WriteFile(path, []byte("old ERROR line\n"), 0o644)

	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	var out bytes.Buffer
	go func() {
		time.Sleep(300 * time.Millisecond)
		f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		f.WriteString("2026-10-14 10:00:00,000 [1] SEVERE - boom\njava.lang.NullPointerException: x\n\tat a.b.C(C.java:1)\nINFO fine\n")
		f.Close()
	}()
	Follow(ctx, FollowOptions{Extra: []string{filepath.Join(dir, "*.log")}}, &out)

	got := out.String()
	for _, want := range []string{"SEVERE - boom", "NullPointerException", "at a.b.C"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output:\n%s", want, got)
		}
	}
	if strings.Contains(got, "old ERROR") || strings.Contains(got, "INFO fine") {
		t.Errorf("printed lines it should skip:\n%s", got)
	}
}
