package mtls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
)

// LoadServerTLSConfig creates a strict mTLS configuration that requires and
// verifies client certificates against the configured CA.
func LoadServerTLSConfig(certFile, keyFile, caFile string) (*tls.Config, error) {
	serverCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load server key pair: %w", err)
	}

	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read client CA file: %w", err)
	}

	clientCAPool := x509.NewCertPool()
	if ok := clientCAPool.AppendCertsFromPEM(caPEM); !ok {
		return nil, fmt.Errorf("parse client CA file: no certificates found")
	}

	return &tls.Config{
		MinVersion:       tls.VersionTLS13,
		Certificates:     []tls.Certificate{serverCert},
		ClientCAs:        clientCAPool,
		ClientAuth:       tls.RequireAndVerifyClientCert,
		CurvePreferences: []tls.CurveID{tls.X25519, tls.CurveP256},
		NextProtos:       []string{"h2", "http/1.1"},
	}, nil
}

// HasVerifiedClientCert checks that TLS state contains a verified client cert.
func HasVerifiedClientCert(r *http.Request) bool {
	if r == nil || r.TLS == nil {
		return false
	}
	if len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 {
		return false
	}
	return true
}
