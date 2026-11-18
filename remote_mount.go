package kvm

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"
)

type RemoteImageReader interface {
	Read(ctx context.Context, offset int64, size int64) ([]byte, error)
}

const diskReplyPrefixSize = 24 // request ID (8 bytes), mount generation (16 bytes)

type webRTCMediaMount struct {
	owner      *Session
	channel    *webrtc.DataChannel
	generation [16]byte
	ctx        context.Context
	cancel     context.CancelFunc
}

func newWebRTCMediaMount(owner *Session, generation string) (*webRTCMediaMount, error) {
	if owner == nil || currentSession.Load() != owner {
		return nil, errors.New("WebRTC media requires the active session")
	}
	channel := owner.diskChannel.Load()
	if channel == nil || channel.ReadyState() != webrtc.DataChannelStateOpen {
		return nil, errors.New("WebRTC disk channel is unavailable")
	}
	mount := &webRTCMediaMount{owner: owner, channel: channel}
	if len(generation) != len(mount.generation)*2 {
		return nil, errors.New("invalid WebRTC media generation")
	}
	if _, err := hex.Decode(mount.generation[:], []byte(generation)); err != nil {
		return nil, errors.New("invalid WebRTC media generation")
	}
	mount.ctx, mount.cancel = context.WithCancel(context.Background())
	return mount, nil
}

type WebRTCDiskReader struct {
	mu      sync.Mutex
	nextID  uint64 // protected by mu
	pending atomic.Pointer[webRTCDiskRead]
}

type webRTCDiskRead struct {
	mount  *webRTCMediaMount
	id     uint64
	chunks chan []byte
	done   chan struct{}
}

func (w *WebRTCDiskReader) receiveChunk(channel *webrtc.DataChannel, data []byte) {
	read := w.pending.Load()
	if read == nil || read.mount.channel != channel || len(data) < diskReplyPrefixSize ||
		binary.BigEndian.Uint64(data[:8]) != read.id ||
		!bytes.Equal(data[8:diskReplyPrefixSize], read.mount.generation[:]) || read.mount.ctx.Err() != nil {
		return
	}
	select {
	case read.chunks <- data[diskReplyPrefixSize:]:
	case <-read.done:
	case <-read.mount.ctx.Done():
	}
}

var webRTCDiskReader WebRTCDiskReader

func (w *WebRTCDiskReader) Read(ctx context.Context, offset int64, size int64) ([]byte, error) {
	state, _ := rpcGetVirtualMediaState()
	return w.readMounted(ctx, state, offset, size)
}

func (w *WebRTCDiskReader) readMounted(ctx context.Context, state *VirtualMediaState, offset, size int64) ([]byte, error) {
	if state == nil || state.webRTC == nil {
		return nil, errors.New("no WebRTC image mounted")
	}
	mount := state.webRTC
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(mount.ctx, cancel)
	defer stop()

	w.mu.Lock()
	defer w.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if mount.ctx.Err() != nil || currentSession.Load() != mount.owner {
		return nil, errors.New("WebRTC media mount is no longer active")
	}
	if offset < 0 || size < 0 {
		return nil, errors.New("negative disk read offset or size")
	}
	if offset >= state.Size || size == 0 {
		return nil, nil
	}
	size = min(size, state.Size-offset)
	if size > math.MaxInt {
		return nil, errors.New("disk read too large")
	}

	w.nextID++
	read := &webRTCDiskRead{
		mount: mount, id: w.nextID, chunks: make(chan []byte, 1), done: make(chan struct{}),
	}
	w.pending.Store(read)
	defer func() {
		w.pending.Store(nil)
		close(read.done)
	}()

	req := DiskReadRequest{
		ID: strconv.FormatUint(read.id, 10), Generation: hex.EncodeToString(mount.generation[:]),
		Start: uint64(offset), End: uint64(offset + size),
	}
	jsonBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if err := mount.channel.SendText(string(jsonBytes)); err != nil {
		return nil, err
	}
	data, err := collectWebRTCDiskChunks(ctx, read.chunks, uint64(offset), int(size))
	if mount.ctx.Err() != nil || currentSession.Load() != mount.owner {
		return nil, errors.New("WebRTC media mount is no longer active")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return data, err
}

func collectWebRTCDiskChunks(ctx context.Context, chunks <-chan []byte, offset uint64, expectedLen int) ([]byte, error) {
	buf := make([]byte, 0, expectedLen)
	for len(buf) < expectedLen {
		select {
		case data := <-chunks:
			payload, err := diskChunkPayload(data, offset+uint64(len(buf)), expectedLen-len(buf))
			if err != nil {
				return buf, err
			}
			buf = append(buf, payload...)
		case <-ctx.Done():
			return buf, ctx.Err()
		}
	}
	return buf, nil
}

func diskChunkPayload(data []byte, expectedOffset uint64, remaining int) ([]byte, error) {
	if len(data) < 16 {
		return nil, errors.New("short WebRTC disk chunk header")
	}
	if binary.BigEndian.Uint64(data[:8]) != expectedOffset {
		// A late response from a previous read may still be in flight.
		return nil, nil
	}
	length := binary.BigEndian.Uint64(data[8:16])
	payload := data[16:]
	if length == 0 || length != uint64(len(payload)) || length > uint64(remaining) {
		return nil, fmt.Errorf("invalid WebRTC disk chunk length %d (payload %d, remaining %d)", length, len(payload), remaining)
	}
	return payload, nil
}

type httpImageReader struct {
	url         string
	resourceURL string
	etag        string
	size        int64
	changed     atomic.Bool
}

func openHTTPImage(rawURL string) (*httpImageReader, error) {
	rawURL = strings.TrimSpace(rawURL)
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("invalid HTTP image URL")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reader := &httpImageReader{url: rawURL}
	var probe [1]byte
	_, size, err := reader.readRange(ctx, probe[:], 0)
	if err != nil {
		return nil, err
	}
	reader.size = size
	return reader, nil
}

// The caller bounds the requested range to the image size before reading.
func (r *httpImageReader) readRange(ctx context.Context, p []byte, off int64) (int, int64, error) {
	if r.changed.Load() {
		return 0, 0, errors.New("HTTP image changed; remount it to continue")
	}
	resourceURL := r.resourceURL
	if resourceURL == "" {
		resourceURL = r.url
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resourceURL, nil)
	if err != nil {
		return 0, 0, err
	}
	byteRange := fmt.Sprintf("%d-%d", off, off+int64(len(p))-1)
	req.Header.Set("Range", "bytes="+byteRange)
	req.Header.Set("Accept-Encoding", "identity")
	if r.etag != "" {
		req.Header.Set("If-Match", r.etag)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusPreconditionFailed {
		r.changed.Store(true)
		return 0, 0, errors.New("HTTP image changed; remount it to continue")
	}
	if resp.StatusCode != http.StatusPartialContent {
		return 0, 0, fmt.Errorf("expected HTTP 206 for image range, got %s", resp.Status)
	}

	etag := resp.Header.Get("ETag")
	responseURL := resp.Request.URL.String()
	if r.etag != "" && (etag != r.etag || responseURL != r.resourceURL) {
		r.changed.Store(true)
		return 0, 0, errors.New("HTTP image changed; remount it to continue")
	}
	if !strongImageETag(etag) {
		return 0, 0, errors.New("HTTP image server must provide a strong ETag")
	}

	rangePrefix := "bytes " + byteRange + "/"
	total, ok := strings.CutPrefix(resp.Header.Get("Content-Range"), rangePrefix)
	size, err := strconv.ParseInt(total, 10, 64)
	if !ok || err != nil || size < off+int64(len(p)) || (r.size != 0 && size != r.size) {
		return 0, 0, errors.New("invalid Content-Range for HTTP image")
	}
	if encoding := resp.Header.Get("Content-Encoding"); encoding != "" && !strings.EqualFold(encoding, "identity") {
		return 0, 0, errors.New("HTTP image range must be uncompressed")
	}
	if resp.ContentLength >= 0 && resp.ContentLength != int64(len(p)) {
		return 0, 0, errors.New("unexpected Content-Length for HTTP image range")
	}
	n, err := io.ReadFull(resp.Body, p)
	if err != nil {
		return n, size, err
	}
	if resp.ContentLength < 0 {
		var extra [1]byte
		if _, err := io.ReadFull(resp.Body, extra[:]); err != io.EOF {
			if err == nil {
				err = errors.New("HTTP image response exceeds requested range")
			}
			return 0, 0, err
		}
	}
	if r.size == 0 {
		r.etag = etag
		r.resourceURL = responseURL
	}
	return n, size, nil
}

func strongImageETag(value string) bool {
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return false
	}
	for i := 1; i < len(value)-1; i++ {
		if b := value[i]; b < 0x21 || b == '"' || b == 0x7f {
			return false
		}
	}
	return true
}
