// +build windows

package main

import (
	"syscall"

	"golang.org/x/sys/windows"
)

func getSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}

func isAdmin() bool {
	var sid *windows.SID
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)

	var token windows.Token
	err = windows.OpenCurrentProcessToken(windows.TOKEN_QUERY, &token)
	if err != nil {
		return false
	}
	defer token.Close()

	member, err := token.IsMember(sid)
	if err != nil {
		return false
	}
	return member
}
