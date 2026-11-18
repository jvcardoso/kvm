package kvm

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const vpnToolsRoot = "/userdata/vpn-tools"
const vntPinnedVersion = "v1.2.16"

var vpnToolActivationMutex sync.Mutex

type vpnToolSpec struct {
	Name          string
	Repo          string
	Binaries      []string
	VersionBinary string
	VersionFlags  [][]string
}

var vpnToolSpecs = map[string]vpnToolSpec{
	"frpc": {
		Name:          "frpc",
		Repo:          "fatedier/frp",
		Binaries:      []string{"frpc"},
		VersionBinary: "frpc",
		VersionFlags:  [][]string{{"-v"}, {"--version"}, {"version"}},
	},
	"easytier": {
		Name:          "easytier",
		Repo:          "EasyTier/EasyTier",
		Binaries:      []string{"easytier-core", "easytier-cli"},
		VersionBinary: "easytier-cli",
		VersionFlags:  [][]string{{"--version"}, {"-V"}, {"version"}},
	},
	"vnt": {
		Name:          "vnt",
		Repo:          "vnt-dev/vnt",
		Binaries:      []string{"vnt-cli"},
		VersionBinary: "vnt-cli",
		VersionFlags:  [][]string{{}, {"--version"}, {"-V"}, {"version"}},
	},
	"cloudflared": {
		Name:          "cloudflared",
		Repo:          "cloudflare/cloudflared",
		Binaries:      []string{"cloudflared"},
		VersionBinary: "cloudflared",
		VersionFlags:  [][]string{{"-v"}, {"version"}, {"--version"}},
	},
	"netbird": {
		Name:          "netbird",
		Repo:          "netbirdio/netbird",
		Binaries:      []string{"netbird"},
		VersionBinary: "netbird",
		VersionFlags:  [][]string{{"version"}},
	},
}

type VpnToolSystemInfo struct {
	GOOS         string   `json:"goos"`
	GOARCH       string   `json:"goarch"`
	UnameArch    string   `json:"uname_arch"`
	ArchLabel    string   `json:"arch_label"`
	ArchKeywords []string `json:"arch_keywords"`
}

type VpnToolStatus struct {
	Tool            string   `json:"tool"`
	Installed       bool     `json:"installed"`
	Source          string   `json:"source"`
	CurrentVersion  string   `json:"current_version"`
	DetectedVersion string   `json:"detected_version"`
	ManagedVersions []string `json:"managed_versions"`
}

type VpnToolReleaseAsset struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	ArchMatch bool   `json:"arch_match"`
}

type VpnToolRelease struct {
	TagName string                `json:"tag_name"`
	Assets  []VpnToolReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
	Size               int64  `json:"size"`
}

type githubRelease struct {
	TagName    string               `json:"tag_name"`
	Draft      bool                 `json:"draft"`
	Prerelease bool                 `json:"prerelease"`
	Assets     []githubReleaseAsset `json:"assets"`
}

type VpnToolInstallTask struct {
	Tool      string   `json:"tool"`
	Running   bool     `json:"running"`
	Progress  float64  `json:"progress"`
	Message   string   `json:"message"`
	Logs      []string `json:"logs"`
	Error     string   `json:"error"`
	Version   string   `json:"version"`
	UpdatedAt int64    `json:"updated_at"`
}

var (
	vpnToolInstallTaskMu sync.Mutex
	vpnToolInstallTasks  = map[string]*VpnToolInstallTask{}
)

func getVpnToolSpec(tool string) (vpnToolSpec, error) {
	spec, ok := vpnToolSpecs[strings.ToLower(strings.TrimSpace(tool))]
	if !ok {
		return vpnToolSpec{}, fmt.Errorf("unsupported vpn tool: %s", tool)
	}
	return spec, nil
}

func normalizeArchLabel(arch string) string {
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "x86_64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	case "armv7l", "armv7", "armhf":
		return "armv7"
	case "armv6l", "armv6":
		return "armv6"
	case "i386", "i686", "386", "x86":
		return "386"
	default:
		return strings.ToLower(strings.TrimSpace(arch))
	}
}

func archKeywords(archLabel string) []string {
	switch archLabel {
	case "amd64":
		return []string{"amd64", "x86_64", "x64"}
	case "arm64":
		return []string{"arm64", "aarch64"}
	case "armv7":
		return []string{"armv7", "armv7l", "armhf", "arm"}
	case "armv6":
		return []string{"armv6", "armv6l", "arm"}
	case "386":
		return []string{"386", "i386", "x86"}
	default:
		if archLabel == "" {
			return []string{}
		}
		return []string{archLabel}
	}
}

func rpcGetVpnToolSystemInfo() (VpnToolSystemInfo, error) {
	unameArch := runtime.GOARCH
	if out, err := exec.Command("uname", "-m").Output(); err == nil {
		unameArch = strings.TrimSpace(string(out))
	}
	archLabel := normalizeArchLabel(unameArch)
	if archLabel == "" {
		archLabel = normalizeArchLabel(runtime.GOARCH)
	}

	return VpnToolSystemInfo{
		GOOS:         runtime.GOOS,
		GOARCH:       runtime.GOARCH,
		UnameArch:    unameArch,
		ArchLabel:    archLabel,
		ArchKeywords: archKeywords(archLabel),
	}, nil
}

func vpnToolDir(spec vpnToolSpec) string {
	return filepath.Join(vpnToolsRoot, spec.Name)
}

func vpnToolVersionsDir(spec vpnToolSpec) string {
	return filepath.Join(vpnToolDir(spec), "versions")
}

func vpnToolCurrentDir(spec vpnToolSpec) string {
	return filepath.Join(vpnToolDir(spec), "current")
}

func managedBinaryPath(spec vpnToolSpec, binaryName string) string {
	return filepath.Join(vpnToolCurrentDir(spec), binaryName)
}

func findExecutablePath(binary string) (string, error) {
	if strings.Contains(binary, "/") {
		info, err := os.Stat(binary)
		if err != nil {
			return "", err
		}
		if info.Mode().IsRegular() || (info.Mode()&os.ModeSymlink) != 0 {
			return binary, nil
		}
		return "", fmt.Errorf("not executable file: %s", binary)
	}
	return exec.LookPath(binary)
}

func resolveVpnToolBinary(tool, defaultBinary string) string {
	spec, err := getVpnToolSpec(tool)
	if err != nil {
		return defaultBinary
	}
	managed := managedBinaryPath(spec, defaultBinary)
	if _, err := os.Stat(managed); err == nil {
		return managed
	}
	return defaultBinary
}

func detectCommandVersion(binaryPath string, flags [][]string) string {
	for _, args := range flags {
		cmd := exec.Command(binaryPath, args...)
		out, err := cmd.CombinedOutput()
		if err != nil && len(out) == 0 {
			continue
		}
		line := extractVersionLine(string(out))
		if line != "" {
			return line
		}
	}
	return ""
}

func extractVersionLine(output string) string {
	normalized := strings.ReplaceAll(output, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")

	// Prefer explicit version lines to handle tools like vnt-cli
	// where the first line is usage text.
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "version:") {
			return line
		}
	}

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "version") || strings.HasPrefix(lower, "v") {
			return line
		}
	}

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line != "" {
			return line
		}
	}
	return ""
}

func listManagedVersions(spec vpnToolSpec) []string {
	versionsDir := vpnToolVersionsDir(spec)
	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		return []string{}
	}
	versions := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			versions = append(versions, e.Name())
		}
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] > versions[j] })
	return versions
}

func currentManagedVersion(spec vpnToolSpec) string {
	currentDir := vpnToolCurrentDir(spec)
	if target, err := os.Readlink(currentDir); err == nil {
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(currentDir), target)
		}
		target = filepath.Clean(target)
		if filepath.Dir(target) == vpnToolVersionsDir(spec) {
			return filepath.Base(target)
		}
		return ""
	}
	// Older installations use a directory containing one symlink per binary.
	for _, binary := range spec.Binaries {
		p := filepath.Join(currentDir, binary)
		target, err := os.Readlink(p)
		if err != nil {
			continue
		}
		abs := target
		if !filepath.IsAbs(target) {
			abs = filepath.Clean(filepath.Join(filepath.Dir(p), target))
		}
		versionsRoot := vpnToolVersionsDir(spec) + string(os.PathSeparator)
		if strings.HasPrefix(abs, versionsRoot) {
			rel := strings.TrimPrefix(abs, versionsRoot)
			parts := strings.Split(rel, string(os.PathSeparator))
			if len(parts) > 0 && parts[0] != "" {
				return parts[0]
			}
		}
	}
	return ""
}

func initVpnToolInstallTask(tool, version string) error {
	vpnToolInstallTaskMu.Lock()
	defer vpnToolInstallTaskMu.Unlock()
	if task, ok := vpnToolInstallTasks[tool]; ok && task.Running {
		return fmt.Errorf("install task is already running for %s", tool)
	}
	vpnToolInstallTasks[tool] = &VpnToolInstallTask{
		Tool:      tool,
		Running:   true,
		Progress:  0,
		Message:   "preparing",
		Logs:      []string{"Preparing install task..."},
		Error:     "",
		Version:   version,
		UpdatedAt: time.Now().Unix(),
	}
	return nil
}

func updateVpnToolInstallTask(tool string, progress float64, message string) {
	vpnToolInstallTaskMu.Lock()
	defer vpnToolInstallTaskMu.Unlock()
	task, ok := vpnToolInstallTasks[tool]
	if !ok {
		return
	}
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	task.Progress = progress
	if strings.TrimSpace(message) != "" {
		task.Message = message
	}
	task.UpdatedAt = time.Now().Unix()
}

func appendVpnToolInstallLog(tool, line string) {
	vpnToolInstallTaskMu.Lock()
	defer vpnToolInstallTaskMu.Unlock()
	task, ok := vpnToolInstallTasks[tool]
	if !ok {
		return
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	task.Logs = append(task.Logs, line)
	if len(task.Logs) > 200 {
		task.Logs = task.Logs[len(task.Logs)-200:]
	}
	task.UpdatedAt = time.Now().Unix()
}

func finishVpnToolInstallTask(tool string, err error) {
	vpnToolInstallTaskMu.Lock()
	defer vpnToolInstallTaskMu.Unlock()
	task, ok := vpnToolInstallTasks[tool]
	if !ok {
		return
	}
	task.Running = false
	task.UpdatedAt = time.Now().Unix()
	if err != nil {
		task.Error = err.Error()
		task.Message = "failed"
		task.Logs = append(task.Logs, "Install failed: "+err.Error())
		return
	}
	task.Progress = 1
	task.Message = "completed"
	task.Error = ""
	task.Logs = append(task.Logs, "Install completed.")
}

func rpcGetVpnToolInstallTask(tool string) (VpnToolInstallTask, error) {
	spec, err := getVpnToolSpec(tool)
	if err != nil {
		return VpnToolInstallTask{}, err
	}
	_ = spec
	vpnToolInstallTaskMu.Lock()
	defer vpnToolInstallTaskMu.Unlock()
	task, ok := vpnToolInstallTasks[tool]
	if !ok {
		return VpnToolInstallTask{
			Tool:      tool,
			Running:   false,
			Progress:  0,
			Message:   "",
			Logs:      []string{},
			Error:     "",
			Version:   "",
			UpdatedAt: time.Now().Unix(),
		}, nil
	}
	cp := *task
	cp.Logs = append([]string{}, task.Logs...)
	return cp, nil
}

func rpcGetVpnToolStatus(tool string) (VpnToolStatus, error) {
	spec, err := getVpnToolSpec(tool)
	if err != nil {
		return VpnToolStatus{}, err
	}

	status := VpnToolStatus{
		Tool:            spec.Name,
		Installed:       false,
		Source:          "none",
		CurrentVersion:  currentManagedVersion(spec),
		DetectedVersion: "",
		ManagedVersions: listManagedVersions(spec),
	}

	primaryBinary := spec.Binaries[0]
	versionBinary := spec.VersionBinary
	if strings.TrimSpace(versionBinary) == "" {
		versionBinary = primaryBinary
	}

	managedPrimary := managedBinaryPath(spec, primaryBinary)
	managedVersionBinary := managedBinaryPath(spec, versionBinary)
	if _, err := os.Stat(managedPrimary); err == nil {
		status.Installed = true
		status.Source = "managed"
		status.DetectedVersion = detectCommandVersion(managedVersionBinary, spec.VersionFlags)
		if status.CurrentVersion == "" {
			status.CurrentVersion = "managed"
		}
		return status, nil
	}

	primaryLookedUp, primaryErr := findExecutablePath(primaryBinary)
	if primaryErr == nil {
		status.Installed = true
		status.Source = "system"
		_ = primaryLookedUp
		versionLookedUp, versionErr := findExecutablePath(versionBinary)
		if versionErr == nil {
			status.DetectedVersion = detectCommandVersion(versionLookedUp, spec.VersionFlags)
		}
	}
	return status, nil
}

func rpcListVpnToolReleases(tool string) ([]VpnToolRelease, error) {
	spec, err := getVpnToolSpec(tool)
	if err != nil {
		return nil, err
	}
	systemInfo, err := rpcGetVpnToolSystemInfo()
	if err != nil {
		return nil, err
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=20", spec.Repo)
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		apiURL,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "kvm-vpn-tool-manager")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("fetch releases failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var ghReleases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&ghReleases); err != nil {
		return nil, fmt.Errorf("failed to parse release json: %w", err)
	}

	keywords := systemInfo.ArchKeywords
	results := make([]VpnToolRelease, 0, len(ghReleases))
	for _, rel := range ghReleases {
		if rel.Draft {
			continue
		}
		if spec.Name == "vnt" {
			tag := strings.TrimSpace(rel.TagName)
			if tag != vntPinnedVersion && tag != strings.TrimPrefix(vntPinnedVersion, "v") {
				continue
			}
		}
		releaseItem := VpnToolRelease{
			TagName: strings.TrimSpace(rel.TagName),
			Assets:  make([]VpnToolReleaseAsset, 0, len(rel.Assets)),
		}
		for _, asset := range rel.Assets {
			nameLower := strings.ToLower(strings.TrimSpace(asset.Name))
			isLinux := strings.Contains(nameLower, "linux")
			archMatch := false
			for _, kw := range keywords {
				if strings.Contains(nameLower, strings.ToLower(kw)) {
					archMatch = true
					break
				}
			}
			if !isLinux {
				archMatch = false
			}
			releaseItem.Assets = append(releaseItem.Assets, VpnToolReleaseAsset{
				Name:      asset.Name,
				URL:       strings.TrimSpace(asset.BrowserDownloadURL),
				ArchMatch: archMatch,
			})
		}
		if releaseItem.TagName != "" {
			results = append(results, releaseItem)
		}
	}
	return results, nil
}

func downloadFileToPath(url, targetPath string, expectedSize int64, onProgress func(downloaded int64, total int64)) error {
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("empty download url")
	}
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		url,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to create download request: %w", err)
	}
	req.Header.Set("User-Agent", "kvm-vpn-tool-manager")
	client := &http.Client{Timeout: 2 * time.Minute, CheckRedirect: checkVpnToolDownloadRedirect}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("download failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	out, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("failed to create target file: %w", err)
	}
	defer out.Close()
	total := resp.ContentLength
	var downloaded int64
	buf := make([]byte, 64*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if int64(n) > expectedSize-downloaded {
				return fmt.Errorf("VPN asset exceeds its published size")
			}
			written, writeErr := out.Write(buf[:n])
			if writeErr != nil {
				return fmt.Errorf("failed to write target file: %w", writeErr)
			}
			if written != n {
				return fmt.Errorf("short write while saving downloaded file")
			}
			downloaded += int64(n)
			if onProgress != nil {
				onProgress(downloaded, total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("failed to read download stream: %w", readErr)
		}
	}
	if downloaded != expectedSize {
		return fmt.Errorf("VPN asset size mismatch: got %d, expected %d", downloaded, expectedSize)
	}
	return out.Close()
}

func extractFromTarGz(archivePath string, targetBinaryNames []string, outDir string) (map[string]bool, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gzReader, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gzReader.Close()
	tr := tar.NewReader(gzReader)

	need := make(map[string]bool, len(targetBinaryNames))
	found := make(map[string]bool, len(targetBinaryNames))
	for _, b := range targetBinaryNames {
		need[b] = true
		found[b] = false
	}

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return found, err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		base := filepath.Base(hdr.Name)
		if !need[base] {
			continue
		}
		if err := writeVpnToolBinary(filepath.Join(outDir, base), tr); err != nil {
			return found, err
		}
		found[base] = true
	}
	if _, err := io.Copy(io.Discard, gzReader); err != nil {
		return found, err
	}
	return found, nil
}

func extractFromZip(archivePath string, targetBinaryNames []string, outDir string) (map[string]bool, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	need := make(map[string]bool, len(targetBinaryNames))
	found := make(map[string]bool, len(targetBinaryNames))
	for _, b := range targetBinaryNames {
		need[b] = true
		found[b] = false
	}

	for _, f := range zr.File {
		base := filepath.Base(f.Name)
		if !need[base] {
			continue
		}
		if !f.Mode().IsRegular() {
			return found, fmt.Errorf("VPN archive entry %s is not a regular file", base)
		}
		src, err := f.Open()
		if err != nil {
			return found, err
		}
		err = writeVpnToolBinary(filepath.Join(outDir, base), src)
		src.Close()
		if err != nil {
			return found, err
		}
		found[base] = true
	}
	return found, nil
}

func writeVpnToolBinary(path string, src io.Reader) error {
	dst, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(dst, src)
	if copyErr != nil {
		dst.Close()
		return copyErr
	}
	if err := dst.Sync(); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}

func ensureBinariesInstalled(found map[string]bool, required []string) error {
	missing := make([]string, 0)
	for _, b := range required {
		if !found[b] {
			missing = append(missing, b)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("required binaries are missing in package: %s", strings.Join(missing, ", "))
	}
	return nil
}

func setupCurrentSymlinks(spec vpnToolSpec, version string) error {
	currentDir := vpnToolCurrentDir(spec)
	versionDir := filepath.Join(vpnToolVersionsDir(spec), version)
	return switchVpnToolCurrent(currentDir, versionDir)
}

func switchVpnToolCurrent(currentDir, versionDir string) error {
	stage, err := os.MkdirTemp(filepath.Dir(currentDir), ".activation-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)

	link := filepath.Join(stage, "current")
	if err := os.Symlink(versionDir, link); err != nil {
		return err
	}
	info, err := os.Lstat(currentDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil && info.IsDir() {
		// Convert the legacy directory and all of its binary links in one swap.
		return exchangeVpnToolPaths(link, currentDir)
	}
	return os.Rename(link, currentDir)
}

func promoteVpnToolPackage(staged, versionDir string, activate func() error) (keepStage bool, err error) {
	_, err = os.Lstat(versionDir)
	reinstall := err == nil
	switch {
	case reinstall:
		err = exchangeVpnToolPaths(staged, versionDir)
	case os.IsNotExist(err):
		err = os.Rename(staged, versionDir)
	}
	if err != nil {
		return false, fmt.Errorf("failed to publish VPN package: %w", err)
	}
	if err := activate(); err != nil {
		if reinstall {
			if rollbackErr := exchangeVpnToolPaths(staged, versionDir); rollbackErr != nil {
				return true, errors.Join(err, fmt.Errorf("previous VPN package retained at %s: %w", staged, rollbackErr))
			}
		} else if rollbackErr := os.Rename(versionDir, staged); rollbackErr != nil {
			return false, errors.Join(err, fmt.Errorf("failed to roll back VPN installation: %w", rollbackErr))
		}
		return false, err
	}
	return false, nil
}

func validVpnToolVersion(version string) bool {
	return version != "" && !strings.HasPrefix(version, ".") &&
		!strings.Contains(version, "..") && !strings.ContainsAny(version, `/\`)
}

func validateVpnToolDownloadURL(spec vpnToolSpec, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") ||
		u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return fmt.Errorf("downloadURL must point to an official GitHub release asset")
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 6 || !strings.EqualFold(strings.Join(parts[:2], "/"), spec.Repo) ||
		parts[2] != "releases" || parts[3] != "download" ||
		parts[4] == "" || parts[4] == "." || parts[4] == ".." ||
		parts[5] == "" || parts[5] == "." || parts[5] == ".." || strings.Contains(u.Path, "\\") {
		return fmt.Errorf("downloadURL does not match an official release asset of %s", spec.Repo)
	}
	return nil
}

func checkVpnToolDownloadRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 || req.URL.Scheme != "https" || req.URL.User != nil {
		return fmt.Errorf("invalid GitHub release download redirect")
	}
	switch req.URL.Host {
	case "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return nil
	default:
		return fmt.Errorf("unexpected GitHub release download redirect host %q", req.URL.Host)
	}
}

func resolveVpnToolAsset(spec vpnToolSpec, version, assetName, downloadURL string) (githubReleaseAsset, error) {
	var asset githubReleaseAsset
	if err := validateVpnToolDownloadURL(spec, downloadURL); err != nil {
		return asset, err
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", spec.Repo, url.PathEscape(version))
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return asset, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "kvm-vpn-tool-manager")
	client := &http.Client{
		Timeout:       20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return asset, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return asset, fmt.Errorf("fetching official release metadata returned %s", resp.Status)
	}
	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return asset, err
	}
	if !release.Draft && release.TagName == version {
		for _, candidate := range release.Assets {
			if candidate.Name == assetName && candidate.BrowserDownloadURL == downloadURL && candidate.Size > 0 {
				return candidate, nil
			}
		}
	}
	return asset, fmt.Errorf("asset is not published in the selected release of %s", spec.Repo)
}

func stageVpnToolPackage(packagePath, assetName string, binaries []string, stage string) error {
	assetLower := strings.ToLower(assetName)
	found := map[string]bool{}
	var err error
	switch {
	case strings.HasSuffix(assetLower, ".tar.gz") || strings.HasSuffix(assetLower, ".tgz"):
		found, err = extractFromTarGz(packagePath, binaries, stage)
	case strings.HasSuffix(assetLower, ".zip"):
		found, err = extractFromZip(packagePath, binaries, stage)
	default:
		if len(binaries) != 1 {
			return fmt.Errorf("single binary install needs exactly one binary")
		}
		input, err := os.Open(packagePath)
		if err != nil {
			return err
		}
		defer input.Close()
		if err := writeVpnToolBinary(filepath.Join(stage, binaries[0]), input); err != nil {
			return err
		}
		found[binaries[0]] = true
	}
	if err != nil {
		return fmt.Errorf("failed to extract VPN package: %w", err)
	}
	if err := ensureBinariesInstalled(found, binaries); err != nil {
		return err
	}
	for _, binary := range binaries {
		info, err := os.Lstat(filepath.Join(stage, binary))
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Mode().Perm()&0111 == 0 {
			return fmt.Errorf("invalid executable %s in VPN package", binary)
		}
	}
	return nil
}

func rpcInstallVpnTool(tool, version, assetName, downloadURL string) error {
	spec, err := getVpnToolSpec(tool)
	if err != nil {
		return err
	}
	version = strings.TrimSpace(version)
	if !validVpnToolVersion(version) {
		return fmt.Errorf("valid version is required")
	}
	if spec.Name == "vnt" && version != vntPinnedVersion && version != strings.TrimPrefix(vntPinnedVersion, "v") {
		return fmt.Errorf("vnt install is temporarily pinned to %s", vntPinnedVersion)
	}
	asset, err := resolveVpnToolAsset(spec, version, strings.TrimSpace(assetName), strings.TrimSpace(downloadURL))
	if err != nil {
		return err
	}

	versionsDir := vpnToolVersionsDir(spec)
	versionDir := filepath.Join(versionsDir, version)
	appendVpnToolInstallLog(tool, "Preparing temporary installation directory...")
	updateVpnToolInstallTask(tool, 0.05, "preparing")
	if err := os.MkdirAll(versionsDir, 0755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(versionsDir, ".install-")
	if err != nil {
		return err
	}
	keepStage := false
	defer func() {
		if !keepStage {
			_ = os.RemoveAll(stage)
		}
	}()
	stagedPackage := filepath.Join(stage, "package")
	if err := os.Mkdir(stagedPackage, 0755); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp("", "vpn-tool-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	appendVpnToolInstallLog(tool, "Downloading release package...")
	if err := downloadFileToPath(asset.BrowserDownloadURL, tmpPath, asset.Size, func(downloaded int64, total int64) {
		if total > 0 {
			ratio := float64(downloaded) / float64(total)
			updateVpnToolInstallTask(tool, 0.1+ratio*0.65, "downloading")
			return
		}
		// Unknown total size, keep showing progress moving slowly.
		updateVpnToolInstallTask(tool, 0.4, "downloading")
	}); err != nil {
		return err
	}
	appendVpnToolInstallLog(tool, "Download completed.")

	// Older GitHub releases omit the digest. Their assets are still bound to the
	// authenticated release metadata and checked against its published size.
	if asset.Digest != "" {
		hash, err := hashFileSHA256(tmpPath)
		if err != nil {
			return err
		}
		if fmt.Sprintf("sha256:%x", hash) != asset.Digest {
			return fmt.Errorf("VPN asset SHA-256 digest mismatch")
		}
	}

	updateVpnToolInstallTask(tool, 0.8, "extracting")
	appendVpnToolInstallLog(tool, "Extracting package...")
	if err := stageVpnToolPackage(tmpPath, asset.Name, spec.Binaries, stagedPackage); err != nil {
		return err
	}

	appendVpnToolInstallLog(tool, "Switching to installed version...")
	updateVpnToolInstallTask(tool, 0.97, "activating")
	vpnToolActivationMutex.Lock()
	keepStage, err = promoteVpnToolPackage(stagedPackage, versionDir, func() error {
		return setupCurrentSymlinks(spec, version)
	})
	vpnToolActivationMutex.Unlock()
	if err != nil {
		return err
	}
	updateVpnToolInstallTask(tool, 1, "completed")
	return nil
}

func rpcStartVpnToolInstall(tool, version, assetName, downloadURL string) error {
	_, err := getVpnToolSpec(tool)
	if err != nil {
		return err
	}

	if err := initVpnToolInstallTask(tool, version); err != nil {
		return err
	}
	go func() {
		err := rpcInstallVpnTool(tool, version, assetName, downloadURL)
		finishVpnToolInstallTask(tool, err)
	}()
	return nil
}

func rpcUseVpnToolVersion(tool, version string) error {
	spec, err := getVpnToolSpec(tool)
	if err != nil {
		return err
	}
	version = strings.TrimSpace(version)
	if !validVpnToolVersion(version) {
		return fmt.Errorf("valid version is required")
	}
	vpnToolActivationMutex.Lock()
	defer vpnToolActivationMutex.Unlock()
	versionDir := filepath.Join(vpnToolVersionsDir(spec), version)
	info, err := os.Stat(versionDir)
	if err != nil {
		return fmt.Errorf("version is not installed: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("invalid version directory")
	}
	for _, b := range spec.Binaries {
		if _, err := os.Stat(filepath.Join(versionDir, b)); err != nil {
			return fmt.Errorf("binary %s is missing in version %s", b, version)
		}
	}
	return setupCurrentSymlinks(spec, version)
}

func rpcUninstallVpnToolVersion(tool, version string) error {
	spec, err := getVpnToolSpec(tool)
	if err != nil {
		return err
	}
	version = strings.TrimSpace(version)
	if !validVpnToolVersion(version) {
		return fmt.Errorf("valid version is required")
	}
	vpnToolActivationMutex.Lock()
	defer vpnToolActivationMutex.Unlock()
	versionDir := filepath.Join(vpnToolVersionsDir(spec), version)
	for _, candidate := range listManagedVersions(spec) {
		if candidate != version {
			if err := setupCurrentSymlinks(spec, candidate); err != nil {
				return err
			}
			return os.RemoveAll(versionDir)
		}
	}
	if currentManagedVersion(spec) == version {
		if err := os.RemoveAll(vpnToolCurrentDir(spec)); err != nil {
			return err
		}
	}
	return os.RemoveAll(versionDir)
}
