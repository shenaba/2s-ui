package network

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTestKeyPair(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDer, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDer}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

// Clients put no IP address in SNI (RFC 6066), so a domain that is an IP has to
// expect an empty server name rather than refuse every handshake.
func TestNewTLSConfigServerName(t *testing.T) {
	certFile, keyFile := writeTestKeyPair(t)
	for _, tc := range []struct {
		domain, sni string
		ok          bool
	}{
		{"", "", true},
		{"", "anything.example", true},
		{"panel.example", "panel.example", true},
		{"panel.example", "PANEL.example", true},
		{"panel.example", "", false},
		{"panel.example", "other.example", false},
		{"192.0.2.1", "", true},
		{"192.0.2.1", "192.0.2.1", true},
		{"192.0.2.1", "192.0.2.2", false},
		{"192.0.2.1", "panel.example", false},
		{"2001:db8::1", "", true},
		{"2001:db8::1", "2001:DB8::1", true},
		{"[2001:db8::1]", "", true},
	} {
		config, err := NewTLSConfig(certFile, keyFile, tc.domain)
		if err != nil {
			t.Fatal(err)
		}
		cert, err := config.GetCertificate(&tls.ClientHelloInfo{ServerName: tc.sni})
		if ok := err == nil && cert != nil; ok != tc.ok {
			t.Errorf("domain %q, SNI %q: accepted=%v, want %v (err %v)", tc.domain, tc.sni, ok, tc.ok, err)
		}
	}
}
