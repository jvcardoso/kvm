package kvm

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"kvm/internal/websecure"
)

const (
	tlsStorePath                     = "/userdata/picokvm/tls"
	webSecureListen                  = ":443"
	webSecureSelfSignedDefaultDomain = "kvm.local"
	webSecureSelfSignedCAName        = "KVM Self-Signed CA"
	webSecureSelfSignedOrganization  = "KVM"
	webSecureSelfSignedOU            = "KVM Self-Signed"
	webSecureCustomCertificateName   = "user-defined"
)

var (
	certStore  *websecure.CertStore
	certSigner *websecure.SelfSigner
)

type TLSState struct {
	Mode        string `json:"mode"`
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"privateKey"`
}

func initCertStore() {
	if certStore != nil {
		websecureLogger.Warn().Msg("TLS store already initialized, it should not be initialized again")
		return
	}
	certStore = websecure.NewCertStore(tlsStorePath, websecureLogger)
	certStore.LoadCertificates()

	certSigner = websecure.NewSelfSigner(
		certStore,
		websecureLogger,
		webSecureSelfSignedDefaultDomain,
		webSecureSelfSignedOrganization,
		webSecureSelfSignedOU,
		webSecureSelfSignedCAName,
	)
}

func getCertificate(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
	switch configSnapshot().TLSMode {
	case "self-signed":
		if isTimeSyncNeeded() || !timeSync.IsSyncSuccess() {
			return nil, fmt.Errorf("time is not synced")
		}
		return certSigner.GetCertificate(info)
	case "custom":
		return certStore.GetCertificate(webSecureCustomCertificateName), nil
	}

	websecureLogger.Info().Msg("TLS mode is disabled but WebSecure is running, returning nil")
	return nil, nil
}

func getTLSState() TLSState {
	s := TLSState{}
	switch configSnapshot().TLSMode {
	case "disabled":
		s.Mode = "disabled"
	case "custom":
		s.Mode = "custom"
		cert := certStore.GetCertificate(webSecureCustomCertificateName)
		if cert != nil {
			var certPEM []byte
			// convert to pem format
			for _, c := range cert.Certificate {
				block := pem.Block{
					Type:  "CERTIFICATE",
					Bytes: c,
				}

				certPEM = append(certPEM, pem.EncodeToMemory(&block)...)
			}
			s.Certificate = string(certPEM)
		}
	case "self-signed":
		s.Mode = "self-signed"
	default:
		s.Mode = "disabled"
	}

	return s
}

var tlsSettingsMutex sync.Mutex

func setTLSState(s TLSState) error {
	tlsSettingsMutex.Lock()
	defer tlsSettingsMutex.Unlock()

	mode := s.Mode
	switch mode {
	case "disabled":
		mode = ""
	case "custom":
		if err, _ := certStore.ValidateAndSaveCertificate(webSecureCustomCertificateName, s.Certificate, s.PrivateKey, true); err != nil {
			return fmt.Errorf("failed to save certificate: %w", err)
		}
	case "self-signed":
	default:
		return fmt.Errorf("invalid TLS mode: %s", mode)
	}

	wasEnabled := configSnapshot().TLSMode != ""
	if err := updateConfig(func(next *Config) { next.TLSMode = mode }); err != nil {
		return fmt.Errorf("failed to save TLS config: %w", err)
	}
	if enabled := mode != ""; enabled != wasEnabled {
		if enabled {
			startWebSecureServer()
		} else {
			stopWebSecureServer()
		}
	}
	return nil
}

var (
	startTLS       = make(chan struct{})
	stopTLS        = make(chan struct{})
	tlsServiceLock = sync.Mutex{}
	tlsStarted     = false
)

// RunWebSecureServer runs a web server with TLS.
func runWebSecureServer() {
	tlsServiceLock.Lock()
	defer tlsServiceLock.Unlock()

	tlsStarted = true
	defer func() {
		tlsStarted = false
	}()

	r := setupRouter()

	server := &http.Server{
		Addr:    webSecureListen,
		Handler: r,
		TLSConfig: &tls.Config{
			MaxVersion:       tls.VersionTLS13,
			CurvePreferences: []tls.CurveID{},
			GetCertificate:   getCertificate,
		},
	}
	websecureLogger.Info().Str("listen", webSecureListen).Msg("Starting websecure server")

	go func() {
		for range stopTLS {
			websecureLogger.Info().Msg("Shutting down websecure server")
			err := server.Shutdown(context.Background())
			if err != nil {
				websecureLogger.Error().Err(err).Msg("failed to shutdown websecure server")
			}
		}
	}()

	err := server.ListenAndServeTLS("", "")
	if !errors.Is(err, http.ErrServerClosed) {
		panic(err)
	}
}

func stopWebSecureServer() {
	if !tlsStarted {
		websecureLogger.Info().Msg("Websecure server is not running, not stopping it")
		return
	}
	stopTLS <- struct{}{}
}

func startWebSecureServer() {
	if tlsStarted {
		websecureLogger.Info().Msg("Websecure server is already running, not starting it again")
		return
	}
	startTLS <- struct{}{}
}

func RunWebSecureServer() {
	// Initialize cert store eagerly so setTLSState can be called
	// via JSON-RPC even when TLS server has not started yet.
	initCertStore()

	for range startTLS {
		websecureLogger.Info().Msg("Starting websecure server, as we have received a start signal")
		go runWebSecureServer()
	}
}
