// Package term adds ANSI colors when stdout is a terminal and NO_COLOR is unset.
package term

import "os"

const (
	Red    = "\033[31m"
	Yellow = "\033[33m"
	Dim    = "\033[2m"
	Bold   = "\033[1m"
	reset  = "\033[0m"
)

var enabled = func() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}()

// Paint wraps s in color if colors are enabled.
func Paint(color, s string) string {
	if !enabled || color == "" {
		return s
	}
	return color + s + reset
}
