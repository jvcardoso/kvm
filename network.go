package kvm

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"kvm/internal/network"
	"kvm/internal/timesync"
	"kvm/internal/udhcpc"

	"github.com/guregu/null/v6"
)

const (
	NetIfName = "eth0"
)

var (
	networkState *network.NetworkInterfaceState
)

func setProxyEnvVar(key, value string) {
	value = strings.TrimSpace(value)
	upperKey := strings.ToUpper(key)
	if value == "" {
		_ = os.Unsetenv(key)
		_ = os.Unsetenv(upperKey)
		return
	}
	_ = os.Setenv(key, value)
	_ = os.Setenv(upperKey, value)
}

func applyProxyEnvironment(networkConfig *network.NetworkConfig) {
	if networkConfig == nil {
		return
	}
	setProxyEnvVar("http_proxy", networkConfig.HTTPProxy.String)
	setProxyEnvVar("https_proxy", networkConfig.HTTPSProxy.String)
	setProxyEnvVar("all_proxy", networkConfig.ALLProxy.String)
}

func networkStateChanged() {
	refreshNetworkState(configSnapshot().NetworkConfig)
}

func refreshNetworkState(cfg *network.NetworkConfig) {
	// do not block the main thread
	go waitCtrlAndRequestDisplayUpdate(true)

	// always restart mDNS when the network state changes
	if mDNS != nil {
		_ = mDNS.SetListenOptions(cfg.GetMDNSMode())
		_ = mDNS.SetLocalNames([]string{
			networkState.GetHostname(),
			networkState.GetFQDN(),
		}, true)
	}
}

func initNetwork() error {
	ensureConfigLoaded()
	configured := *configSnapshot().NetworkConfig
	if configured.IPv4Static != nil {
		settings := *configured.IPv4Static
		configured.IPv4Static = &settings
	}
	if configured.IPv6Static != nil {
		settings := *configured.IPv6Static
		configured.IPv6Static = &settings
	}
	applyProxyEnvironment(&configured)

	state, err := network.NewNetworkInterfaceState(&network.NetworkInterfaceOptions{
		DefaultHostname: GetDefaultHostname(),
		InterfaceName:   NetIfName,
		NetworkConfig:   &configured,
		Logger:          networkLogger,
		OnStateChange: func(state *network.NetworkInterfaceState) {
			networkStateChanged()
		},
		OnInitialCheck: func(state *network.NetworkInterfaceState) {
			networkStateChanged()
		},
		OnDhcpLeaseChange: func(lease *udhcpc.Lease) {
			networkStateChanged()

			session := currentSession.Load()
			if session == nil {
				return
			}

			writeJSONRPCEvent("networkState", networkState.RpcGetNetworkState(), session)
		},
		OnConfigChange: refreshNetworkState,
	})

	if state == nil {
		if err == nil {
			return fmt.Errorf("failed to create NetworkInterfaceState")
		}
		return err
	}

	if err := state.Run(); err != nil {
		return err
	}

	networkState = state

	if cfg := configSnapshot().NetworkConfig; cfg != nil && cfg.PendingReboot.Bool {
		_ = updateConfig(func(next *Config) {
			cfg := *next.NetworkConfig
			cfg.PendingReboot = null.BoolFrom(false)
			next.NetworkConfig = &cfg
		})
	}

	return nil
}

func rpcGetNetworkState() network.RpcNetworkState {
	return networkState.RpcGetNetworkState()
}

func rpcGetNetworkSettings() network.RpcNetworkSettings {
	rpcSettings := networkState.RpcGetNetworkSettings()
	hostname := GetHostname()
	rpcSettings.Hostname = null.NewString(hostname, hostname != "")
	return rpcSettings
}

var networkSettingsMutex sync.Mutex

func rpcSetNetworkSettings(settings network.RpcNetworkSettings) (*network.RpcNetworkSettings, error) {
	networkSettingsMutex.Lock()
	defer networkSettingsMutex.Unlock()

	current := networkState.RpcGetNetworkSettings()
	changedCore := !network.IsSame(current.NetworkConfig, settings.NetworkConfig)
	if changedCore {
		settings.NetworkConfig.PendingReboot = null.BoolFrom(true)
	}

	s := networkState.RpcSetNetworkSettings(settings)
	if s != nil {
		return nil, s
	}
	applied := networkState.RpcGetNetworkSettings()
	before, after := current.NetworkConfig, applied.NetworkConfig
	before.PendingReboot, after.PendingReboot = null.Bool{}, null.Bool{}
	if err := updateConfig(func(next *Config) {
		next.NetworkConfig = &applied.NetworkConfig
		if !network.IsSame(before, after) {
			appliedCopy := applied.NetworkConfig
			next.AppliedNetworkConfig = &appliedCopy
		}
		// If we just reverted to the same core config as applied, clear pending_reboot.
		if next.AppliedNetworkConfig != nil {
			// create copies ignoring PendingReboot
			a := *next.AppliedNetworkConfig
			b := applied.NetworkConfig
			a.PendingReboot = null.Bool{}
			b.PendingReboot = null.Bool{}
			if network.IsSame(a, b) {
				applied.NetworkConfig.PendingReboot = null.BoolFrom(false)
			}
		}
	}); err != nil {
		return nil, err
	}
	applyProxyEnvironment(&applied.NetworkConfig)

	// refresh time sync server lists so custom servers take effect immediately
	if timeSync != nil {
		timeSync.SetNetworkConfig(&applied.NetworkConfig)
	}

	return &network.RpcNetworkSettings{NetworkConfig: applied.NetworkConfig}, nil
}

func rpcGetTimeSyncServers() map[string]interface{} {
	return map[string]interface{}{
		"ntp":  timesync.DefaultNTPServers(),
		"http": timesync.DefaultHTTPUrls(),
	}
}

func rpcRenewDHCPLease() error {
	return networkState.RpcRenewDHCPLease()
}

// rpcRestartDHCPClient drops the current lease and runs a fresh DHCP discover.
func rpcRestartDHCPClient() error {
	return networkState.RpcRestartDHCPClient()
}

func rpcRequestDHCPAddress(ip string) error {
	return networkState.RpcRequestDHCPAddress(ip)
}

const ethernetMacAddressPath = "/userdata/ethaddr.txt"

func rpcSetEthernetMacAddress(macAddress string) (interface{}, error) {
	normalized, err := networkState.SetMACAddress(macAddress)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(ethernetMacAddressPath, []byte(normalized+"\n"), 0644); err != nil {
		return nil, fmt.Errorf("failed to write %s: %w", ethernetMacAddressPath, err)
	}
	return networkState.RpcGetNetworkState(), nil
}
