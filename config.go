package kvm

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"kvm/internal/logging"
	"kvm/internal/network"
	"kvm/internal/usbgadget"
)

type WakeOnLanDevice struct {
	Name       string `json:"name"`
	MacAddress string `json:"macAddress"`
}

type TurnServer struct {
	URL        string `json:"url"`
	Username   string `json:"username"`
	Credential string `json:"credential"`
}

// Constants for keyboard macro limits
const (
	MaxMacrosPerDevice = 25
	MaxStepsPerMacro   = 10
	MaxKeysPerStep     = 10
	MinStepDelay       = 50
	MaxStepDelay       = 2000
)

type KeyboardMacroStep struct {
	Keys      []string `json:"keys"`
	Modifiers []string `json:"modifiers"`
	Delay     int      `json:"delay"`
}

func (s *KeyboardMacroStep) Validate() error {
	if len(s.Keys) > MaxKeysPerStep {
		return fmt.Errorf("too many keys in step (max %d)", MaxKeysPerStep)
	}

	if s.Delay < MinStepDelay {
		s.Delay = MinStepDelay
	} else if s.Delay > MaxStepDelay {
		s.Delay = MaxStepDelay
	}

	return nil
}

type KeyboardMacro struct {
	ID        string              `json:"id"`
	Name      string              `json:"name"`
	Steps     []KeyboardMacroStep `json:"steps"`
	SortOrder int                 `json:"sortOrder,omitempty"`
}

func (m *KeyboardMacro) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("macro name cannot be empty")
	}

	if len(m.Steps) == 0 {
		return fmt.Errorf("macro must have at least one step")
	}

	if len(m.Steps) > MaxStepsPerMacro {
		return fmt.Errorf("too many steps in macro (max %d)", MaxStepsPerMacro)
	}

	for i := range m.Steps {
		if err := m.Steps[i].Validate(); err != nil {
			return fmt.Errorf("invalid step %d: %w", i+1, err)
		}
	}

	return nil
}

type Config struct {
	STUN                 string            `json:"stun"`
	TurnServers          []TurnServer      `json:"turn_servers"`
	JigglerEnabled       bool              `json:"jiggler_enabled"`
	UpdateDownloadProxy  string            `json:"update_download_proxy"`
	HashedPassword       string            `json:"hashed_password"`
	LocalAuthToken       string            `json:"local_auth_token"`
	LocalAuthMode        string            `json:"localAuthMode"` // TODO: fix it with migration
	LocalLoopbackOnly    bool              `json:"local_loopback_only"`
	UsbEnhancedDetection bool              `json:"usb_enhanced_detection"`
	WakeOnLanDevices     []WakeOnLanDevice `json:"wake_on_lan_devices"`
	KeyboardMacros       []KeyboardMacro   `json:"keyboard_macros"`
	KeyboardLayout       string            `json:"keyboard_layout"`
	// KeyboardFormFactor is the physical key arrangement the on-screen keyboard
	// mirrors: ANSI (US), ISO (Europe) or JIS (Japan). It is independent of
	// KeyboardLayout, which selects the character mapping of a language.
	KeyboardFormFactor string `json:"keyboard_form_factor"`
	EdidString         string `json:"hdmi_edid_string"`
	ForceHpd           bool   `json:"force_hpd"`
	ActiveExtension    string `json:"active_extension"`
	DisplayRotation    string `json:"display_rotation"`
	// DisplayBrightnessPercent is the backlight percentage (0-100), the same unit the
	// touchscreen slider and the web UI slider use.
	DisplayBrightnessPercent   int                    `json:"display_brightness_percent"`
	DisplayDimAfterSec         int                    `json:"display_dim_after_sec"`
	DisplayOffAfterSec         int                    `json:"display_off_after_sec"`
	TLSMode                    string                 `json:"tls_mode"` // options: "self-signed", "user-defined", ""
	UsbConfig                  *usbgadget.Config      `json:"usb_config"`
	UsbDevices                 *usbgadget.Devices     `json:"usb_devices"`
	NetworkConfig              *network.NetworkConfig `json:"network_config"`
	AppliedNetworkConfig       *network.NetworkConfig `json:"applied_network_config,omitempty"`
	DefaultLogLevel            string                 `json:"default_log_level"`
	TailScaleAutoStart         bool                   `json:"tailscale_autostart"`
	TailScaleXEdge             bool                   `json:"tailscale_xedge"`
	ZeroTierNetworkID          string                 `json:"zerotier_network_id"`
	ZeroTierAutoStart          bool                   `json:"zerotier_autostart"`
	FrpcAutoStart              bool                   `json:"frpc_autostart"`
	FrpcToml                   string                 `json:"frpc_toml"`
	CloudflaredAutoStart       bool                   `json:"cloudflared_autostart"`
	CloudflaredToken           string                 `json:"cloudflared_token"`
	IO0Status                  bool                   `json:"io0_status"`
	IO1Status                  bool                   `json:"io1_status"`
	AudioMode                  string                 `json:"audio_mode"`
	TimeZone                   string                 `json:"time_zone"`
	LEDGreenMode               string                 `json:"led_green_mode"`
	LEDYellowMode              string                 `json:"led_yellow_mode"`
	AutoMountImage             *AutoMountImageConfig  `json:"auto_mount_image"`
	EasytierAutoStart          bool                   `json:"easytier_autostart"`
	EasytierConfig             EasytierConfig         `json:"easytier_config"`
	VntAutoStart               bool                   `json:"vnt_autostart"`
	VntConfig                  VntConfig              `json:"vnt_config"`
	WireguardAutoStart         bool                   `json:"wireguard_autostart"`
	WireguardConfig            WireguardConfig        `json:"wireguard_config"`
	NetbirdAutoStart           bool                   `json:"netbird_autostart"`
	NetbirdManagementURL       string                 `json:"netbird_management_url"`
	NpuAppEnabled              bool                   `json:"npu_app_enabled"`
	Firewall                   *FirewallConfig        `json:"firewall"`
	APIKey                     string                 `json:"api_key"`                // MCP-only API key, used by mcp.go
	MCPDisabled                bool                   `json:"mcp_disabled,omitempty"` // MCP server off; the zero value keeps it enabled for existing configs
	PersistedVirtualMediaState *VirtualMediaState     `json:"persisted_virtual_media_state,omitempty"`
	SerialBaudRate             int                    `json:"serial_baud_rate,omitempty"`
	SerialDataBits             int                    `json:"serial_data_bits,omitempty"`
	SerialStopBits             string                 `json:"serial_stop_bits,omitempty"`    // "1", "1.5", "2"
	SerialParity               string                 `json:"serial_parity,omitempty"`       // "none", "odd", "even", "mark", "space"
	SerialPortEnabled          bool                   `json:"serial_port_enabled,omitempty"` // user intent: keep the serial port open across reloads and reboots
	// MobileVirtualMouse holds the mobile virtual mouse overlay layout so the
	// widget positions survive reloads and are shared by every browser.
	MobileVirtualMouse  *MobileVirtualMouseConfig `json:"mobile_virtual_mouse,omitempty"`
	StreamQualityFactor float64                   `json:"stream_quality_factor,omitempty"`
	StreamEncodecType   string                    `json:"stream_encodec_type,omitempty"`
	VideoRc             *VideoRcConfigParams      `json:"video_rc,omitempty"`
}

// MobileVirtualMouseConfig is the persisted virtual mouse overlay layout.
type MobileVirtualMouseConfig struct {
	Enabled bool                                `json:"enabled"` // master switch: show the widgets at all
	Speed   float64                             `json:"speed"`   // joystick speed multiplier
	Widgets map[string]MobileVirtualMouseWidget `json:"widgets"`
}

// MobileVirtualMouseWidget is one widget's placement and appearance. Positions
// are fractions of the overlay area so the layout survives any screen size.
type MobileVirtualMouseWidget struct {
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Scale   float64 `json:"scale"`
	Opacity float64 `json:"opacity"`
	Enabled bool    `json:"enabled"`
}

type AutoMountImageConfig struct {
	Filename string `json:"filename"` // e.g. "ubuntu-24.04.iso"
	Source   string `json:"source"`   // "kvm" or "sd"
}

type FirewallConfig struct {
	Base         FirewallBaseRule   `json:"base"`
	Rules        []FirewallRule     `json:"rules"`
	PortForwards []FirewallPortRule `json:"portForwards"`
}

type FirewallBaseRule struct {
	InputPolicy   string `json:"inputPolicy"`
	OutputPolicy  string `json:"outputPolicy"`
	ForwardPolicy string `json:"forwardPolicy"`
}

type FirewallRule struct {
	Chain           string   `json:"chain"`
	SourceIP        string   `json:"sourceIP"`
	SourcePort      *int     `json:"sourcePort,omitempty"`
	Protocols       []string `json:"protocols"`
	DestinationIP   string   `json:"destinationIP"`
	DestinationPort *int     `json:"destinationPort,omitempty"`
	Action          string   `json:"action"`
	Comment         string   `json:"comment"`
}

type FirewallPortRule struct {
	Chain           string   `json:"chain,omitempty"`
	Managed         *bool    `json:"managed,omitempty"`
	SourcePort      int      `json:"sourcePort"`
	Protocols       []string `json:"protocols"`
	DestinationIP   string   `json:"destinationIP"`
	DestinationPort int      `json:"destinationPort"`
	Comment         string   `json:"comment"`
}

type VntConfig struct {
	Token      string `json:"token"`
	DeviceId   string `json:"device_id"`
	Name       string `json:"name"`
	ServerAddr string `json:"server_addr"`
	ConfigMode string `json:"config_mode"` // "params" or "file"
	ConfigFile string `json:"config_file"`
	Model      string `json:"model"`
	Password   string `json:"password"`
}

type WireguardConfig struct {
	NetworkName string `json:"network_name"`
	ConfigFile  string `json:"config_file"`
}

const (
	configPath   = "/userdata/kvm_config.json"
	sdConfigPath = "/mnt/sdcard/kvm_config.json"
)

// builtOtaPublicKey is the hex-encoded Ed25519 public key for OTA signature verification,
// injected via -ldflags at build time. Empty string disables signature verification.
var builtOtaPublicKey = ""

var defaultConfig = &Config{
	STUN:                     "stun:stun.l.google.com:19302",
	TurnServers:              []TurnServer{},
	ActiveExtension:          "",
	KeyboardMacros:           []KeyboardMacro{},
	DisplayRotation:          "180",
	TimeZone:                 "UTC-8",
	KeyboardLayout:           "en_US",
	KeyboardFormFactor:       "ANSI",
	DisplayBrightnessPercent: 32,
	DisplayDimAfterSec:       120,  // 2 minutes
	DisplayOffAfterSec:       1800, // 30 minutes
	TLSMode:                  "",
	ForceHpd:                 false,
	UsbEnhancedDetection:     true,
	UsbConfig: &usbgadget.Config{
		VendorId:     "0x1d6b", // The Linux Foundation
		ProductId:    "0x0104", // Multifunction Composite Gadget
		SerialNumber: "",
		Manufacturer: "KVM",
		Product:      "USB Emulation Device",
	},
	UsbDevices: &usbgadget.Devices{
		AbsoluteMouse: true,
		RelativeMouse: true,
		Keyboard:      true,
		MassStorage:   true,
		Audio:         false, // At any given time, only one of Audio and Mtp can be set to true
		Mtp:           false,
	},
	NetworkConfig:        &network.NetworkConfig{},
	AppliedNetworkConfig: nil,
	DefaultLogLevel:      "INFO",
	ZeroTierAutoStart:    false,
	TailScaleAutoStart:   false,
	TailScaleXEdge:       false,
	FrpcAutoStart:        false,
	CloudflaredAutoStart: false,
	IO0Status:            false,
	IO1Status:            false,
	AudioMode:            "disabled",
	LEDGreenMode:         "network-rx",
	LEDYellowMode:        "kernel-activity",
	AutoMountImage:       &AutoMountImageConfig{Filename: systemInfoImageFilename, Source: "kvm"},
	WireguardAutoStart:   false,
	NpuAppEnabled:        false,
	StreamQualityFactor:  1.0,
	StreamEncodecType:    "avc",
	Firewall: &FirewallConfig{
		Base: FirewallBaseRule{
			InputPolicy:   "accept",
			OutputPolicy:  "accept",
			ForwardPolicy: "accept",
		},
		Rules:        []FirewallRule{},
		PortForwards: []FirewallPortRule{},
	},
}

var (
	configState atomic.Pointer[Config]
	configLock  = &sync.Mutex{}
)

type ConfigMigrationFunc func(raw json.RawMessage) (json.RawMessage, error)

var configMigrations = []ConfigMigrationFunc{
	migrateLocalAuthMode,
	migrateDisplayBrightnessPercent,
	migrateLegacyTruncatedEdid,
}

// migrateLegacyTruncatedEdid replaces the legacy 128-byte default EDID (base
// block only, while byte 126 still announced one extension block) with the full
// 256-byte default. A custom EDID set by the user is left untouched.
func migrateLegacyTruncatedEdid(raw json.RawMessage) (json.RawMessage, error) {
	rawMap, err := parseConfigObject(raw)
	if err != nil {
		return nil, fmt.Errorf("migrateLegacyTruncatedEdid: failed to parse config JSON: %w", err)
	}

	edidRaw, exists := rawMap["hdmi_edid_string"]
	if !exists {
		return json.Marshal(rawMap)
	}

	var edid string
	if err := json.Unmarshal(edidRaw, &edid); err != nil {
		return nil, fmt.Errorf("migrateLegacyTruncatedEdid: failed to parse hdmi_edid_string: %w", err)
	}
	if !strings.EqualFold(edid, legacyTruncatedEdidHex) {
		return json.Marshal(rawMap)
	}

	encoded, err := json.Marshal(defaultEdidHex)
	if err != nil {
		return nil, fmt.Errorf("migrateLegacyTruncatedEdid: failed to encode default EDID: %w", err)
	}
	rawMap["hdmi_edid_string"] = encoded
	return json.Marshal(rawMap)
}

// migrateDisplayBrightnessPercent replaces the legacy raw backlight level
// (display_max_brightness, 0-200) with a percentage (display_brightness_percent),
// so the web UI, the touchscreen slider and the config all share one unit.
func migrateDisplayBrightnessPercent(raw json.RawMessage) (json.RawMessage, error) {
	rawMap, err := parseConfigObject(raw)
	if err != nil {
		return nil, fmt.Errorf("migrateDisplayBrightnessPercent: failed to parse config JSON: %w", err)
	}

	if _, exists := rawMap["display_brightness_percent"]; exists {
		delete(rawMap, "display_max_brightness")
		return json.Marshal(rawMap)
	}

	percent := defaultConfig.DisplayBrightnessPercent
	if levelRaw, exists := rawMap["display_max_brightness"]; exists {
		var level int
		if err := json.Unmarshal(levelRaw, &level); err == nil {
			percent = displayBacklightPercent(level)
		}
		delete(rawMap, "display_max_brightness")
	}

	// The darkest slider position is backlightMinPercent, a legacy "0" level
	// would leave the panel dark with no way to bring it back from the UI.
	if percent < backlightMinPercent {
		percent = backlightMinPercent
	}

	percentRaw, err := json.Marshal(percent)
	if err != nil {
		return nil, fmt.Errorf("migrateDisplayBrightnessPercent: failed to marshal percent: %w", err)
	}
	rawMap["display_brightness_percent"] = percentRaw

	result, err := json.Marshal(rawMap)
	if err != nil {
		return nil, fmt.Errorf("migrateDisplayBrightnessPercent: failed to marshal migrated config: %w", err)
	}
	return result, nil
}

func migrateLocalAuthMode(raw json.RawMessage) (json.RawMessage, error) {
	rawMap, err := parseConfigObject(raw)
	if err != nil {
		return nil, fmt.Errorf("migrateLocalAuthMode: failed to parse config JSON: %w", err)
	}

	if authModeRaw, exists := rawMap["localAuthMode"]; exists {
		var authMode string
		if err := json.Unmarshal(authModeRaw, &authMode); err == nil {
			if authMode != "" {
				validModes := map[string]bool{"password": true, "noPassword": true}
				if !validModes[authMode] {
					rawMap["localAuthMode"] = json.RawMessage(`"password"`)
				}
			}
		} else {
			delete(rawMap, "localAuthMode")
		}
	}

	result, err := json.Marshal(rawMap)
	if err != nil {
		return nil, fmt.Errorf("migrateLocalAuthMode: failed to marshal migrated config: %w", err)
	}
	return result, nil
}

func runMigrations(raw json.RawMessage) (json.RawMessage, bool, error) {
	if _, err := parseConfigObject(raw); err != nil {
		return raw, false, err
	}
	current := raw
	didMigrate := false
	for i, migration := range configMigrations {
		migrated, err := migration(current)
		if err != nil {
			return current, didMigrate, fmt.Errorf("migration %d failed: %w", i, err)
		}
		if string(migrated) != string(current) {
			didMigrate = true
			logger.Info().Int("migration", i).Msg("config migrated")
		}
		current = migrated
	}
	return current, didMigrate, nil
}

func writeRawConfig(path string, raw json.RawMessage) error {
	var indented bytes.Buffer
	if err := json.Indent(&indented, raw, "", "  "); err != nil {
		return fmt.Errorf("invalid config JSON: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".kvm-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err := indented.WriteTo(file); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func parseConfigObject(raw []byte) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, errors.New("configuration must be a JSON object")
	}
	return fields, nil
}

func decodeConfig(raw []byte, requireObjects bool) (*Config, error) {
	if _, err := parseConfigObject(raw); err != nil {
		return nil, err
	}
	// Clone the defaults before decoding so a partial or invalid nested object
	// cannot mutate the defaults or the live configuration.
	defaults, err := json.Marshal(defaultConfig)
	if err != nil {
		return nil, err
	}
	var next Config
	if err := json.Unmarshal(defaults, &next); err != nil {
		return nil, err
	}
	fallback := next
	if err := json.Unmarshal(raw, &next); err != nil {
		return nil, err
	}
	if next.UsbConfig == nil {
		if requireObjects {
			return nil, errors.New("usb_config must be an object")
		}
		next.UsbConfig = fallback.UsbConfig
	}
	if next.UsbDevices == nil {
		if requireObjects {
			return nil, errors.New("usb_devices must be an object")
		}
		next.UsbDevices = fallback.UsbDevices
	}
	if next.NetworkConfig == nil {
		if requireObjects {
			return nil, errors.New("network_config must be an object")
		}
		next.NetworkConfig = fallback.NetworkConfig
	}
	if next.Firewall == nil {
		if requireObjects {
			return nil, errors.New("firewall must be an object")
		}
		next.Firewall = fallback.Firewall
	}
	return &next, nil
}

func loadConfigFile(path string) (loaded *Config, migrated bool, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	updated, changed, err := runMigrations(raw)
	if err == nil {
		loaded, err = decodeConfig(updated, false)
	}
	if err != nil {
		if backupErr := os.Rename(path, path+".corrupt"); backupErr != nil {
			logger.Warn().Err(backupErr).Msg("failed to preserve invalid config")
		}
		return nil, false, err
	}
	if changed {
		if err := writeRawConfig(path, updated); err != nil {
			logger.Warn().Err(err).Msg("failed to persist migrated config")
		} else {
			migrated = true
		}
	}
	return loaded, migrated, nil
}

func LoadConfig() {
	configLock.Lock()
	var migrated bool
	defer func() {
		configLock.Unlock()
		if migrated {
			exportConfigSD()
		}
	}()

	if configSnapshot() != nil {
		return
	}
	if defaultConfig.UsbConfig.SerialNumber == "" {
		serialNumber, err := extractSerialNumber()
		if err != nil {
			logger.Warn().Err(err).Msg("failed to extract serial number")
		} else {
			defaultConfig.UsbConfig.SerialNumber = serialNumber
		}
	}
	loaded, changed, err := loadConfigFile(configPath)
	migrated = changed
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warn().Err(err).Msg("failed to load config, using defaults")
		}
		loaded = new(Config)
		*loaded = *defaultConfig
	}
	if loaded.TurnServers == nil {
		loaded.TurnServers = []TurnServer{}
	}
	if loaded.StreamQualityFactor <= 0 {
		loaded.StreamQualityFactor = defaultConfig.StreamQualityFactor
	}
	if loaded.StreamEncodecType != "avc" && loaded.StreamEncodecType != "hevc" {
		loaded.StreamEncodecType = defaultConfig.StreamEncodecType
	}

	configState.Store(loaded)
	setAppliedStreamFactor(loaded.StreamQualityFactor)
	streamUsesHEVC.Store(loaded.StreamEncodecType == "hevc")
	loadSerialSettingsFromConfig()
	logging.GetRootLogger().UpdateLogLevel(loaded.DefaultLogLevel)
	logger.Info().Str("path", configPath).Msg("config loaded")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	if err := out.Sync(); err != nil {
		return err
	}

	return nil
}

// SD backups carry portable settings. Authentication, network policy, and
// connection or mount targets are managed exclusively in local configuration.
var sdConfigKeys = []string{
	"jiggler_enabled", "usb_enhanced_detection", "wake_on_lan_devices",
	"keyboard_macros", "keyboard_layout", "keyboard_form_factor",
	"hdmi_edid_string", "force_hpd", "active_extension", "display_rotation",
	"display_brightness_percent", "display_dim_after_sec", "display_off_after_sec",
	"io0_status", "io1_status", "audio_mode", "time_zone", "led_green_mode",
	"led_yellow_mode", "serial_baud_rate", "serial_data_bits", "serial_stop_bits",
	"serial_parity", "serial_port_enabled", "mobile_virtual_mouse",
	"stream_quality_factor", "stream_encodec_type", "video_rc",
}

func writeSanitizedConfigToSD(srcPath, dstPath string) error {
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	var local map[string]json.RawMessage
	if err := json.Unmarshal(raw, &local); err != nil {
		return err
	}
	if local == nil {
		return fmt.Errorf("local config must be a JSON object")
	}
	backup := make(map[string]json.RawMessage)
	for _, key := range sdConfigKeys {
		if value, ok := local[key]; ok {
			backup[key] = value
		}
	}
	raw, err = json.Marshal(backup)
	if err != nil {
		return err
	}
	return writeRawConfig(dstPath, raw)
}

func mergeSDConfigIntoUserdata(sdPath, userdataPath string) error {
	sdRaw, err := os.ReadFile(sdPath)
	if err != nil {
		return err
	}
	var sd map[string]json.RawMessage
	if err := json.Unmarshal(sdRaw, &sd); err != nil {
		return err
	}
	if sd == nil {
		return fmt.Errorf("SD config must be a JSON object")
	}
	local := make(map[string]json.RawMessage)
	raw, err := os.ReadFile(userdataPath)
	if err == nil {
		if err := json.Unmarshal(raw, &local); err != nil {
			return err
		}
		// Older builds could save null on first boot.
		if local == nil {
			local = make(map[string]json.RawMessage)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	for _, key := range sdConfigKeys {
		if value, ok := sd[key]; ok {
			local[key] = value
		}
	}
	merged, err := json.Marshal(local)
	if err != nil {
		return err
	}
	var validated Config
	if err := json.Unmarshal(merged, &validated); err != nil {
		return err
	}
	return writeRawConfig(userdataPath, merged)
}

func importConfigSD() {
	resp, err := rpcGetSDMountStatus()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to get SD mount status")
		return
	}
	if resp.Status == SDMountOK {
		if err := mergeSDConfigIntoUserdata(sdConfigPath, configPath); err != nil {
			if _, statErr := os.Stat(sdConfigPath); os.IsNotExist(statErr) {
				return
			}
			logger.Warn().Err(err).Msg("failed to import config from SD")
		}
	}
}

var configSDExportMutex sync.Mutex

func exportConfigSD() {
	configSDExportMutex.Lock()
	defer configSDExportMutex.Unlock()
	resp, err := rpcGetSDMountStatus()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to get SD mount status")
		return
	}
	if resp.Status == SDMountOK {
		if err := writeSanitizedConfigToSD(configPath, sdConfigPath); err != nil {
			logger.Warn().Err(err).Msg("failed to export config to SD")
		}
	}
}

func writeConfig(path string, next *Config) error {
	if next == nil {
		return errors.New("config has not been loaded")
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}
	return writeRawConfig(path, raw)
}

// A published config and its nested values are read-only. Writers copy the
// snapshot and replace any nested value they edit before publishing.
func configSnapshot() *Config {
	return configState.Load()
}

func updateConfig(edit func(*Config)) error {
	if err := commitConfigAt(configPath, edit); err != nil {
		return err
	}
	exportConfigSD()
	return nil
}

func commitConfigAt(path string, edit func(*Config)) error {
	configLock.Lock()
	defer configLock.Unlock()
	current := configSnapshot()
	if current == nil {
		return errors.New("config has not been loaded")
	}
	next := *current
	edit(&next)
	if err := writeConfig(path, &next); err != nil {
		return err
	}
	configState.Store(&next)
	return nil
}

// apiKeyPrefix marks MCP API keys so a leaked key is recognizable on sight.
// Keys generated before this prefix existed stay valid as they are.
const apiKeyPrefix = "lfk_mcp_"

func generateAPIKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return apiKeyPrefix + hex.EncodeToString(bytes), nil
}

func ensureConfigLoaded() {
	if configSnapshot() == nil {
		LoadConfig()
	}
}

var systemInfoWriteLock sync.Mutex

// systemInfoImageFilename is the image auto-mounted by default. Kept in one place
// because the auto-mount logic and the image writer must agree on the name.
const systemInfoImageFilename = "system_info.img"

func writeSystemInfoImg() error {
	systemInfoWriteLock.Lock()
	defer systemInfoWriteLock.Unlock()

	imgPath := filepath.Join(imagesFolder, systemInfoImageFilename)
	unverifiedimgPath := imgPath + ".unverified"
	mountPoint := "/mnt/system_info"

	run := func(cmd string, args ...string) error {
		c := exec.Command(cmd, args...)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Run()
	}

	if _, err := os.Stat(unverifiedimgPath); err == nil {
		err := os.Rename(unverifiedimgPath, imgPath)
		if err != nil {
			return fmt.Errorf("failed to rename %s to %s: %v", unverifiedimgPath, imgPath, err)
		}
		return nil
	}

	isMounted := false
	if f, err := os.Open("/proc/mounts"); err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) >= 2 && fields[1] == mountPoint {
				isMounted = true
				break
			}
		}
	}

	if isMounted {
		logger.Info().Msgf("%s is mounted, umounting...\n", mountPoint)
		_ = run("umount", mountPoint)
	}

	if _, err := os.Stat(mountPoint); err == nil {
		if err := os.Remove(mountPoint); err != nil {
			return fmt.Errorf("failed to remove %s: %v", mountPoint, err)
		}
	}

	if _, err := os.Stat(imgPath); err == nil {
		if err := copyFile(imgPath, unverifiedimgPath); err != nil {
			logger.Error().Err(err).Msg("failed to copy system_info.img")
			return err
		}
	} else {
		if err := run("dd", "if=/dev/zero", "of="+unverifiedimgPath, "bs=1M", "count=4"); err != nil {
			return fmt.Errorf("dd failed: %v", err)
		}

		if err := run("mkfs.vfat", unverifiedimgPath); err != nil {
			return fmt.Errorf("mkfs.vfat failed: %v", err)
		}
	}

	if err := os.MkdirAll(mountPoint, 0o755); err != nil {
		return fmt.Errorf("mkdir failed: %v", err)
	}

	if err := run("mount", "-o", "loop", unverifiedimgPath, mountPoint); err != nil {
		return fmt.Errorf("mount failed: %v", err)
	}

	if err := run("cp", "/etc/hostname", mountPoint+"/hostname.txt"); err != nil {
		return fmt.Errorf("copy hostname failed: %v", err)
	}
	if err := run("sh", "-c", "ip addr show > "+mountPoint+"/network_info.txt"); err != nil {
		return fmt.Errorf("write network info failed: %v", err)
	}

	_ = run("umount", mountPoint)
	if err := os.RemoveAll(mountPoint); err != nil {
		return fmt.Errorf("failed to remove %s: %v", mountPoint, err)
	}

	if err := os.Rename(unverifiedimgPath, imgPath); err != nil {
		return fmt.Errorf("failed to rename %s to %s: %v", unverifiedimgPath, imgPath, err)
	}

	logger.Info().Msg("system_info.img update successfully")
	return nil
}
