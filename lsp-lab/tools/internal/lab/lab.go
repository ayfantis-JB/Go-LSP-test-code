// Package lab drives the docker compose test lab and runs the examples.
package lab

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Service is one compose service.
type Service struct {
	Name    string
	SSHPort int
}

// Services in docker-compose.yml.
var Services = []Service{{"devbox", 2221}, {"runner", 2222}, {"app", 2223}}

// ServiceNames lists the service names.
func ServiceNames() []string {
	names := make([]string, len(Services))
	for i, s := range Services {
		names[i] = s.Name
	}
	return names
}

// Root walks up from the working directory to the directory holding go.mod.
func Root() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found")
		}
		dir = parent
	}
}

// Module returns the module path declared in root/go.mod.
func Module(root string) (string, error) {
	f, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "module "); ok {
			return strings.Trim(rest, `"`), nil
		}
	}
	return "", errors.New("no module directive in go.mod")
}

// KeyPath is the lab's private SSH key.
func KeyPath(root string) string { return filepath.Join(root, "containers", "ssh", "id_ed25519") }

// EnsureKey generates the lab SSH key and publishes the public half to the
// directory the containers mount as authorized keys.
func EnsureKey(root string, out io.Writer) error {
	key := KeyPath(root)
	pubDir := filepath.Join(root, "containers", "ssh", "pub")
	if err := os.MkdirAll(pubDir, 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(key); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(out, "generating", key)
		cmd := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "lsp-lab", "-f", key)
		cmd.Stdout, cmd.Stderr = out, out
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("ssh-keygen: %w", err)
		}
	}
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(pubDir, "lsp-lab.pub"), pub, 0o644)
}

// WriteSSHConfig prints ~/.ssh/config entries for every service.
func WriteSSHConfig(root string, w io.Writer) {
	fmt.Fprintln(w, "# add to ~/.ssh/config (or: go run ./tools/lab ssh-config >> ~/.ssh/config)")
	for _, s := range Services {
		fmt.Fprintf(w, "Host lsp-lab-%s\n  HostName 127.0.0.1\n  Port %d\n  User dev\n  IdentityFile %s\n  IdentitiesOnly yes\n  StrictHostKeyChecking no\n  UserKnownHostsFile /dev/null\n\n",
			s.Name, s.SSHPort, KeyPath(root))
	}
}

// HostTZ returns the host's IANA timezone so container logs line up with host logs.
func HostTZ() string {
	if tz := os.Getenv("TZ"); tz != "" {
		return tz
	}
	if link, err := os.Readlink("/etc/localtime"); err == nil {
		if _, name, ok := strings.Cut(link, "zoneinfo/"); ok {
			return name
		}
	}
	return "UTC"
}

// Compose runs `docker compose args...` in root with stdio attached.
func Compose(root string, args ...string) error {
	cmd := exec.Command("docker", append([]string{"compose"}, args...)...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "TZ="+HostTZ())
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// Up prepares keys and output dirs, then builds and starts all services.
func Up(root string, out io.Writer, build bool) error {
	if err := EnsureKey(root, out); err != nil {
		return err
	}
	for _, s := range Services {
		if err := os.MkdirAll(filepath.Join(root, "lab-out", s.Name), 0o755); err != nil {
			return err
		}
	}
	args := []string{"up", "-d"}
	if build {
		args = append(args, "--build")
	}
	if err := Compose(root, args...); err != nil {
		return err
	}
	WriteSSHConfig(root, out)
	return nil
}

// InstallExt copies a VSIX into each container and installs it there with
// `lab install-ext-local`, which needs VS Code Server to have connected once.
func InstallExt(vsix string, out io.Writer) error {
	if _, err := os.Stat(vsix); err != nil {
		return fmt.Errorf("vsix: %w", err)
	}
	for _, s := range ServiceNames() {
		container := "lsp-lab-" + s + "-1"
		if err := exec.Command("docker", "cp", vsix, container+":/tmp/ext.vsix").Run(); err != nil {
			return fmt.Errorf("%s: docker cp: %w", s, err)
		}
		cmd := exec.Command("docker", "exec", "-u", "dev", container, "lab", "install-ext-local", "/tmp/ext.vsix")
		cmd.Stdout, cmd.Stderr = out, out
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: %w", s, err)
		}
	}
	return nil
}

// InstallExtLocal installs a VSIX with the newest VS Code Server CLI in $HOME.
func InstallExtLocal(vsix string, out io.Writer) error {
	home, _ := os.UserHomeDir()
	clis, _ := filepath.Glob(filepath.Join(home, ".vscode-server", "bin", "*", "bin", "code-server"))
	if len(clis) == 0 {
		host, _ := os.Hostname()
		fmt.Fprintf(out, "%s: no VS Code Server yet - connect VS Code once first\n", host)
		return nil
	}
	sort.Slice(clis, func(i, j int) bool { return modTime(clis[i]).After(modTime(clis[j])) })
	cmd := exec.Command(clis[0], "--install-extension", vsix, "--force")
	cmd.Stdout, cmd.Stderr = out, out
	return cmd.Run()
}

func modTime(path string) time.Time {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// SkipRe excludes packages from vet, test and runs: the intentionally broken
// diagnostics package and generated stress code.
var SkipRe = regexp.MustCompile(`/lsp/diagnostics$|/lsp/stress(/|$)`)

// Packages lists the module's packages minus SkipRe.
func Packages(dir string) ([]string, error) {
	return goList(dir, "{{.ImportPath}}")
}

func goList(dir, format string) ([]string, error) {
	cmd := exec.Command("go", "list", "-e", "-f", format, "./...")
	cmd.Dir, cmd.Env = dir, GoEnv()
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w", err)
	}
	var res []string
	for _, l := range strings.Fields(string(out)) {
		if !SkipRe.MatchString(l) {
			res = append(res, l)
		}
	}
	return res, nil
}
