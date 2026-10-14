// Command labinit is the entrypoint of the lab containers. As root it installs
// the SSH public keys, starts sshd, seeds the scratch module and fixes volume
// ownership, then drops to the "dev" user and execs the container command.
package main

import (
	"fmt"
	"os"
)

const (
	userName    = "dev"
	home        = "/home/dev"
	pubKeyDir   = "/run/ssh-pub"
	scratchTmpl = "/opt/scratch-template"
)

// ownedDirs are named-volume mount points; Docker creates fresh volumes as root.
var ownedDirs = []string{
	home + "/scratch", home + "/go/pkg/mod", home + "/.cache/go-build", home + "/.vscode-server", "/lab-out",
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: labinit command [args...]")
		os.Exit(2)
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "labinit:", err)
		os.Exit(1)
	}
}
