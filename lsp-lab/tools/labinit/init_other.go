//go:build !linux

package main

import "errors"

func run([]string) error { return errors.New("labinit only runs inside the Linux lab containers") }
