package kvm

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func diskTestChunk(offset, length uint64, payload string) []byte {
	packet := make([]byte, 16, 16+len(payload))
	binary.BigEndian.PutUint64(packet[:8], offset)
	binary.BigEndian.PutUint64(packet[8:], length)
	return append(packet, payload...)
}

func TestWebRTCDiskChunks(t *testing.T) {
	for _, tc := range []struct {
		name   string
		packet []byte
	}{
		{"short header", []byte{0}},
		{"oversized length", diskTestChunk(0, ^uint64(0), "a")},
		{"exceeds requested range", diskTestChunk(0, 2, "ab")},
		{"empty chunk", diskTestChunk(0, 0, "")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := diskChunkPayload(tc.packet, 0, 1); err == nil {
				t.Fatal("accepted invalid disk packet")
			}
		})
	}

	chunks := make(chan []byte, 3)
	chunks <- diskTestChunk(2, 1, "x") // stale offset
	chunks <- diskTestChunk(10, 1, "a")
	chunks <- diskTestChunk(11, 2, "bc")
	data, err := collectWebRTCDiskChunks(context.Background(), chunks, 10, 3)
	if err != nil || string(data) != "abc" {
		t.Fatalf("collected %q, %v", data, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := collectWebRTCDiskChunks(ctx, chunks, 0, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestRemoteImageReadWithoutMountReleasesLock(t *testing.T) {
	var reader remoteImageBackend
	if _, err := reader.ReadAt(make([]byte, 1), 0); err == nil {
		t.Fatal("read succeeded without a mount")
	}
	if !virtualMediaStateMutex.TryLock() {
		t.Fatal("ReadAt left the lock held")
	}
	virtualMediaStateMutex.Unlock()
}

func TestRemoteMountModeFailureReleasesOperationLock(t *testing.T) {
	if err := mountRemoteImage(&VirtualMediaState{Source: WebRTC, Filename: "image.iso", Size: 4096, Mode: CDROM}, nil); err == nil {
		t.Fatal("mount succeeded without a USB gadget")
	}
	if !virtualMediaOperationMutex.TryLock() {
		t.Fatal("mount failure left the operation lock held")
	}
	virtualMediaOperationMutex.Unlock()
	if state, _ := rpcGetVirtualMediaState(); state != nil {
		t.Fatal("published a mount after USB mode setup failed")
	}
}

func TestHTTPImageRequiresMatchingRange(t *testing.T) {
	for _, tc := range []struct {
		name, contentRange, body string
		status                   int
	}{
		{"valid", "bytes 0-0/128", "a", 206},
		{"ignored range", "", "ab", 200},
		{"wrong offset", "bytes 1-1/128", "a", 206},
		{"oversized response", "bytes 0-0/128", "ab", 206},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Range") != "bytes=0-0" {
					t.Error("incorrect range probe")
				}
				w.Header().Set("Content-Range", tc.contentRange)
				w.Header().Set("ETag", `"image"`)
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer server.Close()

			image, err := openHTTPImage(server.URL)
			if tc.name == "valid" {
				if err != nil || image.size != 128 {
					t.Fatalf("image probe = %v, %v", image, err)
				}
			} else if err == nil {
				t.Fatal("accepted invalid range response")
			}
		})
	}
}

func TestSDConfigImportsOnlyPortableFields(t *testing.T) {
	for _, tc := range []struct {
		name, localJSON string
	}{
		{"missing", ""},
		{"legacy null", "null"},
		{"empty protected fields", `{"api_key":"","localAuthMode":null}`},
		{"populated protected fields", `{"api_key":"local-key","localAuthMode":"password"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			sd, local := filepath.Join(dir, "sd.json"), filepath.Join(dir, "local.json")
			writeTestFile(t, sd, `{"api_key":"card-key","localAuthMode":"noPassword","keyboard_layout":"pt_BR","mcp_disabled":true,"network_config":{},"unrecognized":true}`)
			if tc.localJSON != "" {
				writeTestFile(t, local, tc.localJSON)
			}
			if err := mergeSDConfigIntoUserdata(sd, local); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(local)
			if err != nil {
				t.Fatal(err)
			}
			var got, old map[string]json.RawMessage
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if tc.localJSON != "" {
				if err := json.Unmarshal([]byte(tc.localJSON), &old); err != nil {
					t.Fatal(err)
				}
			}
			for _, key := range []string{"api_key", "localAuthMode", "mcp_disabled", "network_config", "unrecognized"} {
				if string(got[key]) != string(old[key]) {
					t.Errorf("%s changed from %q to %q", key, old[key], got[key])
				}
			}
			if string(got["keyboard_layout"]) != `"pt_BR"` {
				t.Fatal("portable field was not imported")
			}
		})
	}
}

func TestSDConfigFailurePreservesLocalFile(t *testing.T) {
	dir := t.TempDir()
	sd, local := filepath.Join(dir, "sd.json"), filepath.Join(dir, "local.json")
	const original = `{"api_key":"local-key"}`
	writeTestFile(t, local, original)
	for _, invalid := range []string{"null", "[]", `{"keyboard_layout":123}`} {
		writeTestFile(t, sd, invalid)
		if err := mergeSDConfigIntoUserdata(sd, local); err == nil {
			t.Fatalf("accepted invalid config %q", invalid)
		}
	}
	if err := writeRawConfig(local, json.RawMessage("{")); err == nil {
		t.Fatal("invalid JSON was accepted")
	}
	raw, err := os.ReadFile(local)
	if err != nil || string(raw) != original {
		t.Fatalf("original config changed: %s, %v", raw, err)
	}
	if err := writeRawConfig(local, raw); err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(local)
	if err != nil || stat.Mode().Perm() != 0o600 {
		t.Fatalf("expected mode 0600, got %v, %v", stat, err)
	}
}

func TestVpnToolRequiresOfficialReleaseURL(t *testing.T) {
	spec := vpnToolSpec{Repo: "example/vpn"}
	for _, raw := range []string{
		"https://raw.githubusercontent.com/another/repo/main/tool",
		"https://github.com/another/repo/releases/download/v1/tool",
		"https://release-assets.githubusercontent.com/tool",
		"https://github.com/example/vpn/releases/download/%2e%2e/tool",
	} {
		if err := validateVpnToolDownloadURL(spec, raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if err := validateVpnToolDownloadURL(spec, "https://github.com/example/vpn/releases/download/v1/tool"); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"https://raw.githubusercontent.com/tool", "http://release-assets.githubusercontent.com/tool"} {
		req, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := checkVpnToolDownloadRedirect(req, nil); err == nil {
			t.Errorf("accepted redirect to %s", target)
		}
	}
}

func TestOTAVerificationRequiresActualCheck(t *testing.T) {
	oldKey := builtOtaPublicKey
	t.Cleanup(func() { builtOtaPublicKey = oldKey })
	builtOtaPublicKey = ""
	path := filepath.Join(t.TempDir(), "firmware")
	const firmware = "firmware bytes"
	writeTestFile(t, path+".unverified", firmware)

	unavailable := httptest.NewServer(http.NotFoundHandler())
	defer unavailable.Close()
	if err := verifyFile(context.Background(), path, "", unavailable.URL, nil, nil, nil); err == nil {
		t.Fatal("promoted firmware after signature download failed")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unverified firmware was promoted: %v", err)
	}
	if err := verifyFile(context.Background(), path, "invalid", "", nil, nil, nil); err == nil {
		t.Fatal("accepted bad hash")
	}

	hash := sha256.Sum256([]byte(firmware))
	verified := true
	if err := verifyFile(context.Background(), path, hex.EncodeToString(hash[:]), unavailable.URL, nil, &verified, nil); err != nil || verified {
		t.Fatalf("hash fallback: signatureVerified=%v, err=%v", verified, err)
	}

	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	builtOtaPublicKey = hex.EncodeToString(public)
	writeTestFile(t, path+".unverified", firmware)
	if err := verifyFile(context.Background(), path, hex.EncodeToString(hash[:]), unavailable.URL, nil, nil, nil); err == nil {
		t.Fatal("fell back to hash with signatures required")
	}
	signature := ed25519.Sign(private, hash[:])
	signed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(signature)
	}))
	defer signed.Close()
	if err := verifyFile(context.Background(), path, "", signed.URL, nil, &verified, nil); err != nil || !verified {
		t.Fatalf("signature-only verification: signatureVerified=%v, err=%v", verified, err)
	}
}

func TestLocalOTARequiresVerification(t *testing.T) {
	oldKey := builtOtaPublicKey
	t.Cleanup(func() { builtOtaPublicKey = oldKey })
	builtOtaPublicKey = ""
	path := filepath.Join(t.TempDir(), "update_system.zip")
	const firmware = "system firmware"
	writeTestFile(t, path, firmware)
	if _, err := verifyLocalFirmware(path, path, nil); err == nil {
		t.Fatal("accepted a package without verification metadata")
	}
	hash := sha256.Sum256([]byte(firmware))
	writeTestFile(t, path+".sha256", hex.EncodeToString(hash[:]))
	if signed, err := verifyLocalFirmware(path, path, nil); err != nil || signed {
		t.Fatalf("local hash verification: signed=%v, err=%v", signed, err)
	}
	public, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	builtOtaPublicKey = hex.EncodeToString(public)
	if _, err := verifyLocalFirmware(path, path, nil); err == nil {
		t.Fatal("accepted a hash-only package when signing is required")
	}
}

func TestABNT2TextKeystrokes(t *testing.T) {
	for _, tc := range []struct {
		char rune
		want []textKeystroke
	}{
		{'ç', []textKeystroke{{0x33, 0}}},
		{'Ã', []textKeystroke{{0x34, 0}, {0x04, 2}}},
		{'é', []textKeystroke{{0x2F, 0}, {0x08, 0}}},
		{'?', []textKeystroke{{0x1A, 0x40}}},
		{'[', []textKeystroke{{0x30, 0}}},
		{'{', []textKeystroke{{0x30, 2}}},
		{'^', []textKeystroke{{0x34, 2}, {0x2C, 0}}},
	} {
		got, ok := abnt2TextKeystrokes(nil, tc.char)
		if !ok || !slices.Equal(got, tc.want) {
			t.Errorf("%q: got %v, want %v", tc.char, got, tc.want)
		}
	}
	if err := typeKeyboardText("Olá 🙂", "pt_BR", 0); err == nil {
		t.Fatal("accepted a character absent from ABNT2")
	}
}

func writeTestFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
