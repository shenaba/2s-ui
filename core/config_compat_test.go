package core

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sagernet/sing-box/experimental/deprecated"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/service"
)

// collectManager records deprecation notes instead of writing them to stderr,
// so tests can assert on which ones a config triggers.
type collectManager struct{ notes []deprecated.Note }

func (c *collectManager) ReportDeprecated(feature deprecated.Note) {
	c.notes = append(c.notes, feature)
}

// jsonPath escapes a filesystem path for substitution into a JSON string
// literal. The placeholders sit inside quotes in the testdata, and on Windows
// t.TempDir() hands back "C:\Users\..." -- the backslashes read as escape codes
// and the whole document fails to parse before any of it is exercised.
func jsonPath(t *testing.T, path string) string {
	t.Helper()
	encoded, err := json.Marshal(path)
	if err != nil {
		t.Fatalf("encode path %q: %v", path, err)
	}
	return string(encoded[1 : len(encoded)-1])
}

// writeTestCert generates a throwaway self-signed cert for the TLS-bearing
// inbounds in testdata, returning the certificate and key paths.
func writeTestCert(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "t.example.com"},
		DNSNames:     []string{"t.example.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	writePEM(t, certPath, "CERTIFICATE", der)
	writePEM(t, keyPath, "EC PRIVATE KEY", keyDER)
	return certPath, keyPath
}

func writePEM(t *testing.T, path, blockType string, der []byte) {
	t.Helper()
	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: blockType, Bytes: der}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestConfigCompat starts a box for each config in testdata/configs and reports
// the deprecation notes it triggers. It guards the sing-box upgrade: configs
// s-ui's UI can produce must keep starting, and any new deprecation shows up in
// the test log so it can be migrated before release.
func TestConfigCompat(t *testing.T) {
	certPath, keyPath := writeTestCert(t)
	cacheDir := t.TempDir()

	files, err := filepath.Glob(filepath.Join("testdata", "configs", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no test configs found")
	}

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			data := strings.NewReplacer(
				"__CERT__", jsonPath(t, certPath),
				"__KEY__", jsonPath(t, keyPath),
				"__CACHEDIR__", jsonPath(t, cacheDir),
			).Replace(string(raw))

			ctx := Context(context.Background(), InboundRegistry(), OutboundRegistry(),
				EndpointRegistry(), DNSTransportRegistry(), ServiceRegistry(), CertificateProviderRegistry())
			notes := &collectManager{}
			ctx = service.ContextWith[deprecated.Manager](ctx, notes)

			var opts option.Options
			if err = opts.UnmarshalJSONContext(ctx, []byte(data)); err != nil {
				t.Fatalf("parse: %v", err)
			}

			instance, err := NewBox(Options{Context: ctx, Options: opts})
			if err != nil {
				t.Fatalf("create box: %v", err)
			}
			defer instance.Close()
			if err = instance.Start(); err != nil {
				t.Fatalf("start: %v", err)
			}

			for _, note := range notes.notes {
				t.Logf("deprecated: %s -- %s", note.Name, note.Description)
			}
		})
	}
}

// TestConfigCompatClean builds each config in testdata/clean and asserts it
// raises no deprecation warning. It covers the post-migration shapes produced
// by database.migrateSingBox114, so the migration is known to actually resolve
// what it claims to.
//
// These configs are built but not started, which is what lets them cover cases
// TestConfigCompat cannot: an ACME provider would reach out to Let's Encrypt on
// start, and a bridge outbound needs privileges to create its tun. Every
// deprecation these shapes could raise is reported while the box is built.
func TestConfigCompatClean(t *testing.T) {
	acmeDir := t.TempDir()
	cacheDir := t.TempDir()
	certPath, keyPath := writeTestCert(t)

	files, err := filepath.Glob(filepath.Join("testdata", "clean", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no clean test configs found")
	}

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			data := strings.NewReplacer(
				"__ACMEDIR__", jsonPath(t, acmeDir),
				"__CACHEDIR__", jsonPath(t, cacheDir),
				"__CERT__", jsonPath(t, certPath),
				"__KEY__", jsonPath(t, keyPath),
			).Replace(string(raw))

			ctx := Context(context.Background(), InboundRegistry(), OutboundRegistry(),
				EndpointRegistry(), DNSTransportRegistry(), ServiceRegistry(), CertificateProviderRegistry())
			notes := &collectManager{}
			ctx = service.ContextWith[deprecated.Manager](ctx, notes)

			var opts option.Options
			if err = opts.UnmarshalJSONContext(ctx, []byte(data)); err != nil {
				t.Fatalf("parse: %v", err)
			}
			instance, err := NewBox(Options{Context: ctx, Options: opts})
			skipIfFeatureMissing(t, err)
			if err != nil {
				t.Fatalf("create box: %v", err)
			}
			instance.Close()

			for _, note := range notes.notes {
				t.Errorf("migrated config still reports deprecation %q: %s", note.Name, note.Description)
			}
		})
	}
}

// TestDNSLegacyStrategyConflict pins the one 1.14 DNS deprecation that is not a
// warning: a legacy rule-action strategy in the same config as anything that
// turns legacy DNS mode off -- here ip_version -- is refused outright, and the
// panel retries a refused config every five seconds. Both halves are reachable
// from the DNS rule drawer, so views/Dns.vue warns about the combination; this
// is what says the condition it encodes is still the condition sing-box uses.
func TestDNSLegacyStrategyConflict(t *testing.T) {
	const config = `{
		"log": {"level": "error"},
		"dns": {
			"servers": [{"type": "udp", "tag": "google", "server": "8.8.8.8"}],
			"rules": [
				{"domain": ["example.com"], "action": "route", "server": "google", "strategy": "ipv4_only"},
				{"ip_version": 4, "action": "route", "server": "google"}
			]
		},
		"outbounds": [{"type": "direct", "tag": "direct"}]
	}`

	ctx := Context(context.Background(), InboundRegistry(), OutboundRegistry(),
		EndpointRegistry(), DNSTransportRegistry(), ServiceRegistry(), CertificateProviderRegistry())
	ctx = service.ContextWith[deprecated.Manager](ctx, &collectManager{})

	var opts option.Options
	if err := opts.UnmarshalJSONContext(ctx, []byte(config)); err != nil {
		t.Fatalf("parse: %v", err)
	}

	instance, err := NewBox(Options{Context: ctx, Options: opts})
	if err == nil {
		err = instance.Start()
		instance.Close()
	}
	if err == nil {
		t.Fatal("expected the legacy strategy / ip_version combination to be refused")
	}
	if !strings.Contains(err.Error(), deprecated.OptionLegacyDNSRuleStrategy.Description) {
		t.Fatalf("refused for the wrong reason: %v", err)
	}
}
