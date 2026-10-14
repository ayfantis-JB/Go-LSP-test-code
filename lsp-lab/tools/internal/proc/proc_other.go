//go:build !linux && !darwin

package proc

import "errors"

func list() ([]Sample, error) { return nil, errors.New("proc: unsupported OS") }

func enrich(*Sample) {}
