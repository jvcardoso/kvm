package kvm

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func TestLoadLegacyNullConfigWithoutSD(t *testing.T) {
	for _, sd := range []string{"missing", "invalid", "valid"} {
		t.Run(sd, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.json")
			sdPath := filepath.Join(dir, "sd.json")
			writeTestFile(t, path, "null")
			switch sd {
			case "invalid":
				writeTestFile(t, sdPath, "null")
			case "valid":
				writeTestFile(t, sdPath, `{"keyboard_layout":"pt_BR"}`)
			}
			importErr := mergeSDConfigIntoUserdata(sdPath, path)
			loaded, _, err := loadConfigFile(path)
			if sd == "valid" {
				if importErr != nil || err != nil || loaded.KeyboardLayout != "pt_BR" {
					t.Fatalf("load after SD import: %v, import=%v, load=%v", loaded, importErr, err)
				}
				return
			}
			if importErr == nil || err == nil || loaded != nil {
				t.Fatalf("expected invalid config to yield no config after failed import: %v, %v, %v", loaded, importErr, err)
			}
			old, err := os.ReadFile(path + ".corrupt")
			if err != nil || string(old) != "null" {
				t.Fatalf("legacy config backup: %q, %v", old, err)
			}
		})
	}
}

func TestRawConfigCommitsOnlyAfterPersisting(t *testing.T) {
	original := configSnapshot()
	t.Cleanup(func() { configState.Store(original) })
	start := *defaultConfig
	start.APIKey = "original"
	configState.Store(&start)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	for _, invalid := range []string{"null", "[]", `{"usb_config":null}`, `{"stream_encodec_type":"invalid"}`} {
		if err := replaceRawConfigLocked(path, []byte(invalid)); err == nil || configSnapshot() != &start {
			t.Fatalf("published invalid config %q: %v", invalid, err)
		}
	}
	valid := []byte(`{"keyboard_layout":"pt_BR","api_key":"replacement"}`)
	if err := replaceRawConfigLocked(filepath.Join(dir, "missing", "config.json"), valid); err == nil || configSnapshot() != &start {
		t.Fatalf("published config after save failed: %v", err)
	}
	if err := replaceRawConfigLocked(path, valid); err != nil {
		t.Fatal(err)
	}
	if configSnapshot().KeyboardLayout != "pt_BR" || configSnapshot().APIKey != "original" || configSnapshot().UsbConfig == nil {
		t.Fatal("editor did not preserve defaults and protected credentials")
	}
}

func TestVideoRestoreSerializesWithUpdateAndReportsSaveFailure(t *testing.T) {
	initialConfig := configSnapshot()
	initialFactor := appliedStreamFactor.Load()
	initialCodec := streamUsesHEVC.Load()
	t.Cleanup(func() {
		configState.Store(initialConfig)
		appliedStreamFactor.Store(initialFactor)
		streamUsesHEVC.Store(initialCodec)
	})
	start := *defaultConfig
	start.StreamQualityFactor = 1
	configState.Store(&start)

	client, server := net.Pipe()
	ctrlConnWriteLock.Lock()
	previous := ctrlSocketConn
	ctrlSocketConn = client
	ctrlConnWriteLock.Unlock()
	t.Cleanup(func() {
		client.Close()
		server.Close()
		ctrlConnWriteLock.Lock()
		ctrlSocketConn = previous
		ctrlConnWriteLock.Unlock()
	})
	actions := make(chan CtrlAction, 8)
	releaseCodec := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseCodec) }) }
	defer release()
	go func() {
		decoder := json.NewDecoder(server)
		for {
			var action CtrlAction
			if err := decoder.Decode(&action); err != nil {
				return
			}
			actions <- action
			if action.Action == "set_video_encodec_type" {
				<-releaseCodec
			}
			deliverCtrlResponse(action.Seq, &CtrlResponse{})
		}
	}()
	nextAction := func() CtrlAction {
		t.Helper()
		select {
		case action := <-actions:
			return action
		case <-time.After(time.Second):
			t.Fatal("native action timed out")
			return CtrlAction{}
		}
	}
	restoreDone := make(chan struct{})
	go func() {
		restoreVideoSettingsToNative()
		close(restoreDone)
	}()
	if action := nextAction(); action.Action != "set_video_encodec_type" {
		t.Fatalf("first action: %v", action)
	}

	path := filepath.Join(t.TempDir(), "config.json")
	changeFactor := func(path string, factor float64) error {
		return applyAndPersistVideoSetting(path, func() error {
			_, err := CallCtrlAction("set_video_quality_factor", map[string]interface{}{"quality_factor": factor})
			if err == nil {
				setAppliedStreamFactor(factor)
			}
			return err
		}, func(next *Config) { next.StreamQualityFactor = factor })
	}
	updated := make(chan error, 1)
	go func() { updated <- changeFactor(path, 0.5) }()
	release()
	if action := nextAction(); action.Params["quality_factor"] != float64(1) {
		t.Fatalf("restore did not send the previous quality first: %v", action)
	}
	if action := nextAction(); action.Params["quality_factor"] != 0.5 {
		t.Fatalf("update did not send the latest quality last: %v", action)
	}
	<-restoreDone
	if err := <-updated; err != nil {
		t.Fatal(err)
	}

	if err := changeFactor(filepath.Join(path, "invalid"), 0.25); err == nil || !strings.Contains(err.Error(), "temporarily") {
		t.Fatalf("expected explicit save failure after native success: %v", err)
	}
	actualFactor, _ := rpcGetStreamQualityFactor()
	if actualFactor != 0.25 || configSnapshot().StreamQualityFactor != 0.5 {
		t.Fatalf("applied=%v persisted=%v", actualFactor, configSnapshot().StreamQualityFactor)
	}
	loaded, _, err := loadConfigFile(path)
	if err != nil || loaded.StreamQualityFactor != 0.5 {
		t.Fatalf("load persisted quality: %v, %v", loaded, err)
	}
}

func TestWebRTCRejectsRepliesFromOtherRequestsMountsAndChannels(t *testing.T) {
	currentChannel, oldChannel := new(webrtc.DataChannel), new(webrtc.DataChannel)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mount := &webRTCMediaMount{channel: currentChannel, generation: [16]byte{2}, ctx: ctx}
	read := &webRTCDiskRead{mount: mount, id: 103, chunks: make(chan []byte, 1), done: make(chan struct{})}
	var reader WebRTCDiskReader
	reader.pending.Store(read)
	packet := func(id uint64, generation [16]byte, body string) []byte {
		prefix := make([]byte, diskReplyPrefixSize)
		binary.BigEndian.PutUint64(prefix, id)
		copy(prefix[8:], generation[:])
		return append(prefix, diskTestChunk(0, uint64(len(body)), body)...)
	}
	reader.receiveChunk(currentChannel, packet(102, mount.generation, "old request at same offset"))
	reader.receiveChunk(currentChannel, packet(103, [16]byte{1}, "old mount"))
	reader.receiveChunk(oldChannel, packet(103, mount.generation, "old session"))
	if len(read.chunks) != 0 {
		t.Fatal("accepted stale WebRTC media")
	}
	reader.receiveChunk(currentChannel, packet(103, mount.generation, "new"))
	data, err := collectWebRTCDiskChunks(ctx, read.chunks, 0, 3)
	if err != nil || string(data) != "new" {
		t.Fatalf("valid packet: %q, %v", data, err)
	}
	cancel()
	reader.receiveChunk(currentChannel, packet(103, mount.generation, "late"))
	if len(read.chunks) != 0 {
		t.Fatal("accepted packet after mount invalidation")
	}
}

func TestHTTPImagePinsStrongETagAndDetectsReplacement(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.Header().Set("ETag", `"original"`)
			w.Header().Set("Content-Range", "bytes 0-0/4")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = fmt.Fprint(w, "O")
			return
		}
		if r.Header.Get("If-Match") != `"original"` {
			t.Error("range read omitted mount ETag")
		}
		w.Header().Set("ETag", `"replacement"`)
		w.Header().Set("Content-Range", "bytes 0-3/4")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = fmt.Fprint(w, "BAD!")
	}))
	defer server.Close()
	reader, err := openHTTPImage(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if n, _, err := reader.readRange(context.Background(), make([]byte, 4), 0); n != 0 || err == nil {
		t.Fatalf("accepted replacement image: n=%v, err=%v", n, err)
	}
	if !reader.changed.Load() {
		t.Fatal("replacement did not invalidate the mounted image")
	}
}

func TestHTTPImageRejectsWeakETagAndOversizedChunkedBody(t *testing.T) {
	for _, tc := range []struct {
		etag, body string
		valid      bool
	}{
		{`W/"weak"`, "a", false},
		{"", "a", false},
		{`"strong"`, "ab", false},
		{`"strong"`, "a", true},
	} {
		t.Run(tc.etag+tc.body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("ETag", tc.etag)
				w.Header().Set("Content-Range", "bytes 0-0/16")
				w.WriteHeader(http.StatusPartialContent)
				w.(http.Flusher).Flush()
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			if _, err := openHTTPImage(server.URL); (err == nil) != tc.valid {
				t.Fatalf("expected valid=%t, got %v", tc.valid, err)
			}
		})
	}
}

func TestVPNInstallStagesExtractionAndRestoresOldVersion(t *testing.T) {
	dir := t.TempDir()
	staged, version, current := filepath.Join(dir, "stage"), filepath.Join(dir, "version"), filepath.Join(dir, "current")
	for _, path := range []string{staged, version, current} {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(version, "tool"), "working")
	if err := os.Symlink(filepath.Join(version, "tool"), filepath.Join(current, "tool")); err != nil {
		t.Fatal(err)
	}
	assertCurrent := func(want string) {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(current, "tool"))
		if err != nil || string(data) != want {
			t.Fatalf("active VPN tool: got %q, %v; want %q", data, err, want)
		}
	}
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	archive := tar.NewWriter(gz)
	if err := archive.WriteHeader(&tar.Header{Name: "tool", Mode: 0o755, Size: 32}); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write([]byte("partial")); err != nil {
		t.Fatal(err)
	}
	_ = archive.Close()
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "incomplete.tar.gz")
	if err := os.WriteFile(path, compressed.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stageVpnToolPackage(path, "incomplete.tar.gz", []string{"tool"}, staged); err == nil {
		t.Fatal("accepted incomplete archive")
	}
	assertCurrent("working")

	writeTestFile(t, filepath.Join(staged, "tool"), "new")
	activationErr := errors.New("activation failed")
	if keep, err := promoteVpnToolPackage(staged, version, func() error { return activationErr }); keep || !errors.Is(err, activationErr) {
		t.Fatalf("failed reinstall result: keep=%v, err=%v", keep, err)
	}
	assertCurrent("working")
	if keep, err := promoteVpnToolPackage(staged, version, func() error { return switchVpnToolCurrent(current, version) }); keep || err != nil {
		t.Fatalf("successful reinstall result: keep=%v, err=%v", keep, err)
	}
	assertCurrent("new")
	if _, err := os.Readlink(current); err != nil {
		t.Fatalf("legacy current directory was not atomically migrated: %v", err)
	}
}

func TestConfigSerializesConcurrentSettingsAndReaders(t *testing.T) {
	original := configSnapshot()
	t.Cleanup(func() { configState.Store(original) })
	initial := *defaultConfig
	initial.STUN = "stun:initial"
	initial.KeyboardLayout = "en_US"
	configState.Store(&initial)
	path := filepath.Join(t.TempDir(), "config.json")

	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, edit := range []func(*Config){
		func(next *Config) { next.STUN = "stun:first" },
		func(next *Config) { next.STUN = "stun:second" },
		func(next *Config) { next.KeyboardLayout = "pt_BR" },
	} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < 5; i++ {
				if err := commitConfigAt(path, edit); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 100; i++ {
			cfg, _ := rpcGetRtcServersConfig()
			if !strings.HasPrefix(cfg.STUN, "stun:") {
				t.Errorf("invalid STUN snapshot: %q", cfg.STUN)
			}
			if _, err := rpcGetConfigRaw(); err != nil {
				t.Error(err)
			}
		}
	}()
	close(start)
	wg.Wait()

	cfg := configSnapshot()
	if cfg.STUN == "stun:initial" || cfg.KeyboardLayout != "pt_BR" || initial.STUN != "stun:initial" {
		t.Fatalf("lost config edit or mutated a published snapshot: %+v", cfg)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved Config
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.STUN != cfg.STUN || saved.KeyboardLayout != cfg.KeyboardLayout {
		t.Fatalf("saved config differs from active snapshot: %+v", saved)
	}
	if err := commitConfigAt(filepath.Join(path, "invalid"), func(next *Config) { next.STUN = "stun:unsaved" }); err == nil {
		t.Fatal("expected the write to fail")
	}
	if configSnapshot() != cfg {
		t.Fatal("published an unsaved edit")
	}
}

func TestNativeActionIncludesBlockedWriteAndReleasesConnection(t *testing.T) {
	client, server := net.Pipe()
	ctrlConnWriteLock.Lock()
	previous := ctrlSocketConn
	ctrlSocketConn = client
	ctrlConnWriteLock.Unlock()
	t.Cleanup(func() {
		client.Close()
		server.Close()
		ctrlConnWriteLock.Lock()
		ctrlSocketConn = previous
		ctrlConnWriteLock.Unlock()
	})

	const budget = 120 * time.Millisecond
	ctrlConnWriteLock.Lock()
	_, err := callCtrlActionUntil("test_wait", nil, time.Now().Add(budget))
	ctrlConnWriteLock.Unlock()
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expected timeout waiting for connection lock, got %v", err)
	}

	start := time.Now()
	// No peer is reading the pipe, so the write itself must time out.
	if _, err := callCtrlActionUntil("test_stalled", nil, start.Add(budget)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expected socket write timeout, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*budget {
		t.Fatalf("stalled action exceeded its deadline: %v", elapsed)
	}
	ongoingRequestsLock.Lock()
	pending := len(ongoingRequests)
	ongoingRequestsLock.Unlock()
	if pending != 0 {
		t.Fatalf("left %d native requests pending after timeout", pending)
	}

	go func() {
		var action CtrlAction
		if err := json.NewDecoder(server).Decode(&action); err == nil {
			deliverCtrlResponse(action.Seq, &CtrlResponse{})
		}
	}()
	if _, err := callCtrlActionUntil("test_recovered", nil, time.Now().Add(time.Second)); err != nil {
		t.Fatalf("connection did not recover after timeout: %v", err)
	}
}
