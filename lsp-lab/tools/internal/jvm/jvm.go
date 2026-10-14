// Package jvm talks to the language server's JVM through the bundled jcmd.
package jvm

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrTimeout means jcmd did not answer: the JVM is likely stuck at a safepoint or deadlocked.
var ErrTimeout = errors.New("jcmd timed out")

var serverDirRe = regexp.MustCompile(`(\S*/server)/bin/\S+`)

// FindJcmd locates jcmd in the server's bundled JBR, falling back to PATH.
func FindJcmd(cmd string) string {
	if m := serverDirRe.FindStringSubmatch(cmd); m != nil {
		for _, rel := range []string{"jbr/Contents/Home/bin/jcmd", "jbr/bin/jcmd"} {
			p := filepath.Join(m[1], rel)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	p, _ := exec.LookPath("jcmd")
	return p
}

// Jcmd runs one diagnostic command against pid.
func Jcmd(jcmd string, pid int, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, jcmd, append([]string{strconv.Itoa(pid)}, args...)...).CombinedOutput()
	if ctx.Err() != nil {
		return string(out), ErrTimeout
	}
	return string(out), err
}

var heapRes = []*regexp.Regexp{
	regexp.MustCompile(`total(?: reserved)? \d+K, committed (\d+)K, used (\d+)K`), // G1
	regexp.MustCompile(`total (\d+)K, used (\d+)K`),                               // parallel/serial
}

// Heap is the JVM heap usage in MB.
type Heap struct{ UsedMB, CommittedMB float64 }

// HeapInfo parses GC.heap_info.
func HeapInfo(jcmd string, pid int) (Heap, error) {
	out, err := Jcmd(jcmd, pid, 15*time.Second, "GC.heap_info")
	if errors.Is(err, ErrTimeout) {
		return Heap{}, err
	}
	return ParseHeapInfo(out)
}

// ParseHeapInfo extracts used/committed from GC.heap_info output.
func ParseHeapInfo(out string) (Heap, error) {
	for _, re := range heapRes {
		if m := re.FindStringSubmatch(out); m != nil {
			committed, _ := strconv.ParseFloat(m[1], 64)
			used, _ := strconv.ParseFloat(m[2], 64)
			return Heap{UsedMB: used / 1024, CommittedMB: committed / 1024}, nil
		}
	}
	return Heap{}, fmt.Errorf("unrecognised GC.heap_info output: %.80q", out)
}

// SnapshotOptions selects the expensive parts of a snapshot.
type SnapshotOptions struct {
	Histogram, HeapDump bool
}

// Snapshot writes diagnostics for pid into a new directory under outDir.
// Without jcmd (not a JVM) it falls back to OS-level stack sampling.
func Snapshot(outDir string, pid int, jcmd, reason string, opt SnapshotOptions) (string, error) {
	dir := filepath.Join(outDir, fmt.Sprintf("snap-%d-%s-%s", pid, time.Now().Format("20060102-150405"), reason))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	type job struct {
		file    string
		timeout time.Duration
		cmd     []string
	}
	p := strconv.Itoa(pid)
	jobs := []job{{"ps.txt", 10 * time.Second, []string{"ps", "-o", "pid,ppid,stat,rss,vsz,etime,time,command", "-p", p}}}
	if jcmd != "" {
		jobs = append(jobs,
			job{"threads.txt", 30 * time.Second, []string{jcmd, p, "Thread.print", "-l"}},
			job{"heap_info.txt", 30 * time.Second, []string{jcmd, p, "GC.heap_info"}},
			job{"vm_flags.txt", 30 * time.Second, []string{jcmd, p, "VM.flags"}},
			job{"native_memory.txt", 30 * time.Second, []string{jcmd, p, "VM.native_memory", "summary"}},
		)
		if opt.Histogram {
			jobs = append(jobs, job{"class_histogram.txt", 60 * time.Second, []string{jcmd, p, "GC.class_histogram"}})
		}
		if opt.HeapDump {
			jobs = append(jobs, job{"heapdump.log", 5 * time.Minute, []string{jcmd, p, "GC.heap_dump", filepath.Join(dir, "heap.hprof")}})
		}
	} else if runtime.GOOS == "darwin" {
		jobs = append(jobs, job{"sample.txt", 30 * time.Second, []string{"sample", p, "3"}})
	} else {
		jobs = append(jobs, job{"stack.txt", 5 * time.Second, []string{"cat", "/proc/" + p + "/stack"}})
	}
	if runtime.GOOS == "darwin" {
		jobs = append(jobs, job{"lsof.txt", 30 * time.Second, []string{"lsof", "-nP", "-p", p}})
	}
	for _, j := range jobs {
		ctx, cancel := context.WithTimeout(context.Background(), j.timeout)
		out, err := exec.CommandContext(ctx, j.cmd[0], j.cmd[1:]...).CombinedOutput()
		cancel()
		if err != nil {
			out = append(out, fmt.Sprintf("\n[%s: %v]\n", strings.Join(j.cmd, " "), err)...)
		}
		if err := os.WriteFile(filepath.Join(dir, j.file), out, 0o644); err != nil {
			return dir, err
		}
	}
	return dir, nil
}

// ClassStat is one GC.class_histogram row.
type ClassStat struct{ Instances, Bytes int64 }

var histoRe = regexp.MustCompile(`^\s*\d+:\s+(\d+)\s+(\d+)\s+(\S+)`)

// ReadHistogram parses a class histogram file, or <dir>/class_histogram.txt.
func ReadHistogram(path string) (map[string]ClassStat, error) {
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		path = filepath.Join(path, "class_histogram.txt")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	res := map[string]ClassStat{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if m := histoRe.FindStringSubmatch(sc.Text()); m != nil {
			n, _ := strconv.ParseInt(m[1], 10, 64)
			b, _ := strconv.ParseInt(m[2], 10, 64)
			res[m[3]] = ClassStat{n, b}
		}
	}
	return res, sc.Err()
}

// ClassDelta is the growth of one class between two histograms.
type ClassDelta struct {
	Class                 string
	DeltaBytes, DeltaInst int64
	NowBytes              int64
}

// DiffHistograms returns classes sorted by byte growth, largest first.
func DiffHistograms(old, cur map[string]ClassStat) []ClassDelta {
	res := make([]ClassDelta, 0, len(cur))
	for cls, c := range cur {
		o := old[cls]
		res = append(res, ClassDelta{cls, c.Bytes - o.Bytes, c.Instances - o.Instances, c.Bytes})
	}
	sort.Slice(res, func(i, j int) bool { return res[i].DeltaBytes > res[j].DeltaBytes })
	return res
}
