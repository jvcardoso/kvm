package kvm

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"time"

	"github.com/pojntfx/go-nbd/pkg/server"
	"github.com/rs/zerolog"
)

type remoteImageBackend struct {
	state  *VirtualMediaState
	reader *httpImageReader
}

func (r remoteImageBackend) ReadAt(p []byte, off int64) (n int, err error) {
	state, reader := r.state, r.reader
	if state == nil {
		return 0, errors.New("image not mounted")
	}
	if off < 0 {
		return 0, errors.New("negative disk read offset")
	}
	if len(p) == 0 {
		return 0, nil
	}
	if off >= state.Size {
		return 0, io.EOF
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	readLen := min(int64(len(p)), state.Size-off)
	switch state.Source {
	case WebRTC:
		data, err := webRTCDiskReader.readMounted(ctx, state, off, readLen)
		n = copy(p, data)
		if err == nil && n < len(p) {
			err = io.EOF
		}
		return n, err
	case HTTP:
		if reader == nil {
			return 0, errors.New("HTTP image reader unavailable")
		}
		n, _, err = reader.readRange(ctx, p[:readLen], off)
		if err == nil && n < len(p) {
			err = io.EOF
		}
		return n, err
	default:
		return 0, errors.New("unknown image source")
	}
}

func (r remoteImageBackend) WriteAt(p []byte, off int64) (n int, err error) {
	return 0, errors.New("not supported")
}

func (r remoteImageBackend) Size() (int64, error) {
	if r.state == nil {
		return 0, errors.New("no virtual media state")
	}
	return r.state.Size, nil
}

func (r remoteImageBackend) Sync() error {
	return nil
}

const nbdSocketPath = "/var/run/nbd.socket"
const nbdDevicePath = "/dev/nbd0"

type NBDDevice struct {
	backend    remoteImageBackend
	listener   net.Listener
	serverConn net.Conn
	clientConn net.Conn
	dev        *os.File

	l *zerolog.Logger
}

func NewNBDDevice(backend remoteImageBackend) *NBDDevice {
	return &NBDDevice{backend: backend}
}

func (d *NBDDevice) Start() error {
	var err error

	if _, err := os.Stat(nbdDevicePath); os.IsNotExist(err) {
		return errors.New("NBD device does not exist")
	}

	d.dev, err = os.Open(nbdDevicePath)
	if err != nil {
		return err
	}

	if d.l == nil {
		scopedLogger := nbdLogger.With().
			Str("socket_path", nbdSocketPath).
			Str("device_path", nbdDevicePath).
			Logger()
		d.l = &scopedLogger
	}

	// Remove the socket file if it already exists
	if _, err := os.Stat(nbdSocketPath); err == nil {
		if err := os.Remove(nbdSocketPath); err != nil {
			return err
		}
	}

	d.listener, err = net.Listen("unix", nbdSocketPath)
	if err != nil {
		return err
	}

	d.clientConn, err = net.Dial("unix", nbdSocketPath)
	if err != nil {
		return err
	}

	d.serverConn, err = d.listener.Accept()
	if err != nil {
		return err
	}
	go d.runServerConn()
	go d.runClientConn()
	return nil
}

func (d *NBDDevice) runServerConn() {
	err := server.Handle(
		d.serverConn,
		[]*server.Export{
			{
				Name:        "kvm",
				Description: "",
				Backend:     &d.backend,
			},
		},
		&server.Options{
			ReadOnly:           true,
			MinimumBlockSize:   uint32(1024),
			PreferredBlockSize: uint32(4 * 1024),
			MaximumBlockSize:   uint32(16 * 1024),
			SupportsMultiConn:  false,
		})

	d.l.Info().Err(err).Msg("nbd server exited")
}
