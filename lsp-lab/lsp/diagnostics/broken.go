// Package diagnostics is INTENTIONALLY BROKEN. Every marked line should
// show a diagnostic in the editor. It is excluded from builds and tests
// (see lab.SkipRe in tools/internal/lab), so don't "fix" it.
package diagnostics

import (
	"fmt"
	"os" // want: "os" imported and not used
)

func typeMismatch() int {
	return "not an int" // want: cannot use "not an int" as int value
}

func undefinedName() {
	fmt.Println(doesNotExist) // want: undefined: doesNotExist
}

func unusedVar() {
	x := 42 // want: declared and not used: x
}

func wrongArgs() {
	fmt.Sprintf("%d", "string") // want (vet/printf): wrong type for %d
	strings.ToUpper("x")        // want: undefined: strings (quick fix: add import)
}

type Speaker interface{ Speak() string }

type Dog struct{}

var _ Speaker = Dog{} // want: Dog does not implement Speaker (quick fix: implement missing method)
