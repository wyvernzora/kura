//go:build unix

package main

import "syscall"

func setProcessUmask(mode int) error {
	syscall.Umask(mode)
	return nil
}

func currentProcessUmask() int {
	mode := syscall.Umask(0)
	syscall.Umask(mode)
	return mode
}
