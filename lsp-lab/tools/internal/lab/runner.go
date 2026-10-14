package lab

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// GoEnv is the environment for go commands and example binaries. It enables
// GOEXPERIMENT=jsonv2: gobyexample/json imports encoding/json/v2, which Go 1.26
// only provides behind that experiment.
func GoEnv(extra ...string) []string {
	env := os.Environ()
	exp := os.Getenv("GOEXPERIMENT")
	if !slices.Contains(strings.Split(exp, ","), "jsonv2") {
		if exp != "" {
			exp += ","
		}
		env = append(env, "GOEXPERIMENT="+exp+"jsonv2")
	}
	return append(env, extra...)
}

// Go runs the go tool in dir with output to out.
func Go(dir string, out io.Writer, args ...string) error {
	cmd := exec.Command("go", args...)
	cmd.Dir, cmd.Stdout, cmd.Stderr, cmd.Env = dir, out, out, GoEnv()
	return cmd.Run()
}

type mainPkg struct {
	importPath, rel, dir, bin string
}

// RunExamples vets and tests the module, then builds and runs every main
// package (except the lab's own tools) according to Examples.
func RunExamples(dir string, out io.Writer) error {
	mod, err := Module(dir)
	if err != nil {
		return err
	}
	failed, err := check(dir, mod, out)
	if err != nil {
		return err
	}
	fail := func(what string) { failed = append(failed, what) }

	mains, err := listMains(dir, mod)
	if err != nil {
		return err
	}
	binDir, err := os.MkdirTemp("", "lab-examples-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(binDir)
	fmt.Fprintf(out, "== go build (%d main packages)\n", len(mains))
	buildErrs := buildAll(dir, binDir, mains)

	for _, m := range mains {
		spec := Examples[m.rel]
		fmt.Fprintln(out, "== run", m.rel, strings.Join(spec.Args, " "))
		if berr, ok := buildErrs[m.rel]; ok {
			fmt.Fprintln(out, berr)
			fail("build " + m.rel)
			continue
		}
		if spec.Skip != "" {
			fmt.Fprintln(out, "-- skipped:", spec.Skip)
			continue
		}
		if err := runOne(m, spec, out); err != nil {
			if spec.AllowFail != "" {
				fmt.Fprintf(out, "-- failed (tolerated: %s): %v\n", spec.AllowFail, err)
				continue
			}
			fmt.Fprintln(out, "-- FAIL:", err)
			fail("run " + m.rel)
		}
	}
	if len(failed) > 0 {
		fmt.Fprintln(out, "== FAILURES:", strings.Join(failed, "; "))
		return fmt.Errorf("%d step(s) failed", len(failed))
	}
	fmt.Fprintln(out, "== all OK")
	return nil
}

func listMains(dir, mod string) ([]mainPkg, error) {
	lines, err := goList(dir, `{{if eq .Name "main"}}{{.ImportPath}}={{.Dir}}{{end}}`)
	if err != nil {
		return nil, err
	}
	var res []mainPkg
	for _, l := range lines {
		ip, d, _ := strings.Cut(l, "=")
		if strings.Contains(ip, "/tools/") { // the lab's own commands are vetted and tested, not run
			continue
		}
		rel := strings.TrimPrefix(ip, mod+"/")
		res = append(res, mainPkg{importPath: ip, rel: rel, dir: d})
	}
	sort.Slice(res, func(i, j int) bool { return res[i].rel < res[j].rel })
	return res, nil
}

// buildAll compiles mains in parallel; the result maps rel path -> build output on failure.
func buildAll(dir, binDir string, mains []mainPkg) map[string]string {
	var mu sync.Mutex
	errs := map[string]string{}
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i := range mains {
		m := &mains[i]
		m.bin = filepath.Join(binDir, strings.ReplaceAll(m.rel, "/", "__"))
		if Examples[m.rel].Skip != "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var buf bytes.Buffer
			if err := Go(dir, &buf, "build", "-o", m.bin, m.importPath); err != nil {
				mu.Lock()
				errs[m.rel] = buf.String()
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return errs
}

func runOne(m mainPkg, spec Example, out io.Writer) error {
	if spec.Setup != nil {
		if err := spec.Setup(); err != nil {
			return fmt.Errorf("setup: %w", err)
		}
	}
	cmd := exec.Command(m.bin, spec.Args...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = m.dir, out, out
	cmd.Stdin = strings.NewReader(spec.Stdin)
	cmd.Env = GoEnv(spec.Env...)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	if spec.Interrupt == 0 {
		select {
		case err := <-done:
			return checkExit(err, spec.Exit, out)
		case <-time.After(time.Minute):
			cmd.Process.Kill()
			return errors.New("timed out after 1m")
		}
	}

	var probeErr error
	if spec.Probe != nil {
		probeErr = spec.Probe()
	}
	select {
	case err := <-done:
		if !spec.SignalSelf {
			return fmt.Errorf("exited before being interrupted: %v", err)
		}
		return checkExit(err, spec.Exit, out)
	case <-time.After(spec.Interrupt):
	}
	cmd.Process.Signal(os.Interrupt)
	select {
	case err := <-done:
		if spec.SignalSelf {
			return checkExit(err, spec.Exit, out)
		}
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		<-done
		if spec.SignalSelf {
			return errors.New("ignored SIGINT")
		}
	}
	if probeErr != nil {
		return fmt.Errorf("probe: %w", probeErr)
	}
	fmt.Fprintln(out, "-- probe ok, stopped with SIGINT")
	return nil
}

func checkExit(err error, want int, out io.Writer) error {
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		return err
	}
	if code != want {
		return fmt.Errorf("exit code %d, want %d", code, want)
	}
	if want != 0 {
		fmt.Fprintf(out, "-- exited %d as expected\n", code)
	}
	return nil
}

// Check runs go vet (honouring VetFlags) and go test over Packages.
func Check(dir string, out io.Writer) error {
	mod, err := Module(dir)
	if err != nil {
		return err
	}
	failed, err := check(dir, mod, out)
	if err != nil {
		return err
	}
	if len(failed) > 0 {
		return fmt.Errorf("failed: %s", strings.Join(failed, "; "))
	}
	return nil
}

func check(dir, mod string, out io.Writer) (failed []string, err error) {
	pkgs, err := Packages(dir)
	if err != nil {
		return nil, err
	}
	// vet: one pass for normal packages, one per package with special flags.
	special := map[string]bool{}
	for rel := range VetFlags {
		special[mod+"/"+rel] = true
	}
	plain := slices.DeleteFunc(slices.Clone(pkgs), func(p string) bool { return special[p] })
	fmt.Fprintln(out, "== go vet")
	if Go(dir, out, append([]string{"vet"}, plain...)...) != nil {
		failed = append(failed, "go vet")
	}
	for rel, flags := range VetFlags {
		if Go(dir, out, append(append([]string{"vet"}, flags...), mod+"/"+rel)...) != nil {
			failed = append(failed, "go vet "+rel)
		}
	}
	fmt.Fprintln(out, "== go test")
	if Go(dir, out, append([]string{"test"}, pkgs...)...) != nil {
		failed = append(failed, "go test")
	}
	return failed, nil
}
