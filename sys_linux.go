// +build !windows

package main

import (
	"os"
	"syscall"
)

func getSysProcAttr() *syscall.SysProcAttr {
	return nil
}

func isAdmin() bool {
	return os.Geteuid() == 0
}
