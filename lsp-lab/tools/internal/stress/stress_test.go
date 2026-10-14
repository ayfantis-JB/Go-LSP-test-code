package stress

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateBuilds(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module stresstest\n\ngo 1.26\n"), 0o644)
	if _, err := Generate(root, "stresstest", 5); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", "./"+Dir+"/main")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "depth 4") {
		t.Errorf("unexpected output %q", out)
	}
}
