//go:build darwin

package kvm

import "golang.org/x/sys/unix"

func exchangeVpnToolPaths(a, b string) error {
	return unix.RenameatxNp(unix.AT_FDCWD, a, unix.AT_FDCWD, b, unix.RENAME_SWAP)
}
