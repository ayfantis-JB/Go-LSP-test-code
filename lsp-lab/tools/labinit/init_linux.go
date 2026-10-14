package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func run(args []string) error {
	u, err := user.Lookup(userName)
	if err != nil {
		return err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	if err := installKeys(uid, gid); err != nil {
		fmt.Fprintln(os.Stderr, "labinit:", err)
	}
	if out, err := exec.Command("/usr/sbin/sshd").CombinedOutput(); err != nil {
		return fmt.Errorf("sshd: %v: %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, "scratch", "go.mod")); os.IsNotExist(err) {
		if err := copyTree(scratchTmpl, filepath.Join(home, "scratch"), uid, gid); err != nil {
			fmt.Fprintln(os.Stderr, "labinit: seed scratch:", err)
		}
	}
	for _, d := range ownedDirs {
		os.Chown(d, uid, gid) // best effort: missing dirs are fine
	}

	bin, err := exec.LookPath(args[0])
	if err != nil {
		return err
	}
	// Drop privileges: groups first, then gid, then uid.
	if err := syscall.Setgroups([]int{gid}); err != nil {
		return err
	}
	if err := syscall.Setgid(gid); err != nil {
		return err
	}
	if err := syscall.Setuid(uid); err != nil {
		return err
	}
	env := append(os.Environ(), "HOME="+home, "USER="+userName, "LOGNAME="+userName)
	return syscall.Exec(bin, args, env)
}

// installKeys concatenates /run/ssh-pub/*.pub into authorized_keys.
func installKeys(uid, gid int) error {
	pubs, _ := filepath.Glob(filepath.Join(pubKeyDir, "*.pub"))
	if len(pubs) == 0 {
		return fmt.Errorf("no public keys in %s - SSH login disabled (run `go run ./tools/lab ssh-key`)", pubKeyDir)
	}
	var keys strings.Builder
	for _, p := range pubs {
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		keys.Write(b)
		if !strings.HasSuffix(keys.String(), "\n") {
			keys.WriteByte('\n')
		}
	}
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, "authorized_keys")
	if err := os.WriteFile(path, []byte(keys.String()), 0o600); err != nil {
		return err
	}
	os.Chown(dir, uid, gid)
	return os.Chown(path, uid, gid)
}

func copyTree(src, dst string, uid, gid int) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dst, strings.TrimPrefix(path, src))
		if d.IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			return os.Chown(target, uid, gid)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		return os.Chown(target, uid, gid)
	})
}
