// +build windows

package main

import (
	"syscall"
)

func getSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}

func isAdmin() bool {
	var sid *syscall.SID
	err := syscall.AllocateAndInitializeSid(
		&syscall.SECURITY_NT_AUTHORITY,
		2,
		syscall.SECURITY_BUILTIN_DOMAIN_RID,
		syscall.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid)
	if err != nil {
		return false
	}
	defer syscall.FreeSid(sid)
	token := syscall.Token(0)
	member, err := token.IsMember(sid)
	if err != nil {
		return false
	}
	return member
}
