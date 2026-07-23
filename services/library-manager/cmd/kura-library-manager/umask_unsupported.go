//go:build !unix

package main

import "fmt"

func setProcessUmask(mode int) error {
	return fmt.Errorf("%s is not supported on this platform", envUmask)
}

func currentProcessUmask() int {
	return 0
}
