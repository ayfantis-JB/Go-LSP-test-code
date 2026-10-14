// Scratch module living in a named volume inside the devbox container
// (/home/dev/scratch). Not part of the host workspace: edit, `go run .`, break things.
package main

import (
	"fmt"
	"strings"
)

type greeter interface {
	Greet(name string) string
}

type loud struct{}

func (loud) Greet(name string) string { return strings.ToUpper("hello, " + name + "!") }

func main() {
	var g greeter = loud{}
	fmt.Println(g.Greet("lsp"))
}
