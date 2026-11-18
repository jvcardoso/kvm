//go:build !linux && !darwin

package kvm

import "errors"

func exchangeVpnToolPaths(a, b string) error {
	return errors.New("atomic VPN tool replacement is unsupported on this platform")
}
