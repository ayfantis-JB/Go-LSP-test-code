package lab

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Example says how to run a main package that is not a plain "run and exit"
// program. Keys of Examples are paths relative to the module root.
type Example struct {
	Args  []string
	Stdin string
	Env   []string
	Exit  int          // expected exit code
	Setup func() error // runs before the binary starts
	// Interrupt: the program runs until stopped. After Interrupt it gets
	// SIGINT; it passes if it was still alive and Probe (if any) succeeded.
	Interrupt time.Duration
	Probe     func() error
	// SignalSelf: SIGINT is the program's input, it must then exit with Exit.
	SignalSelf bool
	AllowFail  string // reason a failure is tolerated (e.g. needs internet)
	Skip       string // reason it is not run at all
}

// Examples covers every main package that needs arguments, input, setup, an
// expected failure or a server lifecycle. Everything else runs with no input
// and must exit 0.
var Examples = map[string]Example{
	"gobyexample/command-line-arguments":   {Args: []string{"a", "b", "c", "d"}},
	"gobyexample/command-line-flags":       {Args: []string{"-word=opt", "-numb=7", "-fork", "-svar=flag", "a1", "a2"}},
	"gobyexample/command-line-subcommands": {Args: []string{"foo", "-enable", "-name=joe", "a1", "a2"}},
	"gobyexample/line-filters":             {Stdin: "hello\nfilter\n"},
	"gobyexample/reading-files":            {Setup: writeTempFile("dat", "hello\ngo\n")},
	"gobyexample/exit":                     {Exit: 3},
	"gobyexample/panic":                    {Exit: 2},
	"gobyexample/signals":                  {SignalSelf: true, Interrupt: time.Second},
	"gobyexample/http-server":              {Interrupt: 2 * time.Second, Probe: httpProbe("http://127.0.0.1:8090/hello", "hello")},
	"gobyexample/context":                  {Interrupt: 4 * time.Second, Probe: httpProbe("http://127.0.0.1:8090/hello", "hello")},
	"gobyexample/tcp-server":               {Interrupt: 2 * time.Second, Probe: tcpProbe("127.0.0.1:8090", "Hello from lab")},
	"gobyexample/http-client":              {AllowFail: "needs internet access to gobyexample.com"},
	"gobyexample/testing-and-benchmarking": {Skip: "test-only package, covered by go test"},
	"lsp/cmd/server": {Env: []string{"ADDR=127.0.0.1:18080"}, Interrupt: 2 * time.Second,
		Probe: httpProbe("http://127.0.0.1:18080/shapes", "total_area")},
}

// VetFlags are extra `go vet` flags for packages whose findings are intentional.
var VetFlags = map[string][]string{
	"gobyexample/panic": {"-unreachable=false"}, // code after panic() on purpose
}

// writeTempFile creates os.TempDir()/name, where the examples look for it.
func writeTempFile(name, content string) func() error {
	return func() error { return os.WriteFile(filepath.Join(os.TempDir(), name), []byte(content), 0o644) }
}

// httpProbe GETs url (retrying while the server starts) and expects want in the body.
func httpProbe(url, want string) func() error {
	return func() error {
		var lastErr error
		for range 20 {
			resp, err := http.Get(url)
			if err != nil {
				lastErr = err
				time.Sleep(100 * time.Millisecond)
				continue
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if !strings.Contains(string(body), want) {
				return fmt.Errorf("GET %s: body %q does not contain %q", url, body, want)
			}
			return nil
		}
		return fmt.Errorf("GET %s: %w", url, lastErr)
	}
}

// tcpProbe sends one line and expects the server's reply to mention it.
func tcpProbe(addr, msg string) func() error {
	return func() error {
		var conn net.Conn
		var err error
		for range 20 {
			if conn, err = net.DialTimeout("tcp", addr, time.Second); err == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if err != nil {
			return err
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		if _, err := fmt.Fprintln(conn, msg); err != nil {
			return err
		}
		reply, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil && reply == "" {
			return err
		}
		if !strings.Contains(strings.ToUpper(reply), strings.ToUpper(msg)) {
			return fmt.Errorf("reply %q does not echo %q", reply, msg)
		}
		return nil
	}
}
