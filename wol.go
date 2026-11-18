package kvm

import (
	"bytes"
	"encoding/binary"
	"net"
)

// SendWOLMagicPacket sends a Wake-on-LAN magic packet to the specified MAC address
func rpcSendWOLMagicPacket(macAddress string) error {
	// Parse the MAC address
	mac, err := net.ParseMAC(macAddress)
	if err != nil {
		return ErrorfL(wolLogger, "invalid MAC address", err)
	}

	// Create the magic packet
	packet := createMagicPacket(mac)

	targets := collectWOLBroadcastTargets()
	sentCount := 0
	var lastErr error

	for _, target := range targets {
		conn, dialErr := net.Dial("udp4", target)
		if dialErr != nil {
			lastErr = dialErr
			continue
		}
		_, writeErr := conn.Write(packet)
		conn.Close()
		if writeErr != nil {
			lastErr = writeErr
			continue
		}
		sentCount++
	}

	if sentCount == 0 {
		return ErrorfL(wolLogger, "failed to send WOL packet", lastErr)
	}

	wolLogger.Info().Str("mac", macAddress).Int("targets", sentCount).Msg("WOL packet sent")

	return nil
}

// collectWOLBroadcastTargets returns 255.255.255.255:9 plus the directed IPv4
// broadcast addresses of active non-loopback network interfaces (e.g., eth0),
// so Wake-on-LAN works reliably even when VPN interfaces alter the default route.
func collectWOLBroadcastTargets() []string {
	seen := map[string]bool{"255.255.255.255:9": true}
	targets := []string{"255.255.255.255:9"}

	ifaces, err := net.Interfaces()
	if err != nil {
		return targets
	}

	for _, iface := range ifaces {
		if (iface.Flags&net.FlagUp) == 0 || (iface.Flags&net.FlagBroadcast) == 0 || (iface.Flags&net.FlagLoopback) != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP == nil {
				continue
			}
			ip4 := ipNet.IP.To4()
			mask := ipNet.Mask
			if ip4 == nil || len(mask) != net.IPv4len {
				continue
			}
			bcast := make(net.IP, net.IPv4len)
			for i := 0; i < net.IPv4len; i++ {
				bcast[i] = ip4[i] | ^mask[i]
			}
			target := bcast.String() + ":9"
			if !seen[target] {
				seen[target] = true
				targets = append(targets, target)
			}
		}
	}

	return targets
}

// createMagicPacket creates a Wake-on-LAN magic packet
func createMagicPacket(mac net.HardwareAddr) []byte {
	var buf bytes.Buffer

	// Write 6 bytes of 0xFF
	buf.Write(bytes.Repeat([]byte{0xFF}, 6))

	// Write the target MAC address 16 times
	for i := 0; i < 16; i++ {
		_ = binary.Write(&buf, binary.BigEndian, mac)
	}

	return buf.Bytes()
}
