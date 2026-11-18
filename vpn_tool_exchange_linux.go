//go:build linux

package kvm

import "golang.org/x/sys/unix"

func exchangeVpnToolPaths(a, b string) error {
	return unix.Renameat2(unix.AT_FDCWD, a, unix.AT_FDCWD, b, unix.RENAME_EXCHANGE)
}
