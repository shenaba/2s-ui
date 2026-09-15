package util

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The body belongs to an operator-configured URL, and this runs on every
// subscription request. An unbounded ReadAll lets a redirect to a large file --
// or a source that simply never stops -- take the process's memory with it.
//
// What does not fit is refused rather than truncated: half a subscription
// served as if it were the whole one silently drops the operator's routes.
func TestGetExternalLinkRefusesAnOversizedBody(t *testing.T) {
	line := "trojan://pw@e.com:443#node\n"
	body := strings.Repeat(line, maxExternalSubBytes/len(line)+64)
	if len(body) <= maxExternalSubBytes {
		t.Fatalf("the test body is %d bytes, which is not over the %d cap", len(body), maxExternalSubBytes)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	if got := GetExternalLink(srv.URL); got != "" {
		t.Errorf("got %d bytes back, want the oversized body refused", len(got))
	}
}

// The response becomes the outbound configurations this panel serves to its own
// subscribers, so whoever can intercept the fetch chooses the servers every one
// of those clients connects to. InsecureSkipVerify was set on this call, which
// made that free.
//
// httptest's TLS server presents a certificate signed by nobody, which is the
// same thing a MITM presents.
func TestGetExternalLinkRefusesAnUntrustedCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("trojan://pw@e.com:443#node\n"))
	}))
	defer srv.Close()

	if got := GetExternalLink(srv.URL); got != "" {
		t.Errorf("got %q, want nothing: the certificate is signed by nobody", got)
	}
}

// And a subscription of an ordinary size still comes through whole, so the
// above is not passing because the fetch stopped working.
func TestGetExternalLinkReadsAnOrdinaryBody(t *testing.T) {
	const body = "trojan://pw@e.com:443#one\nvless://11111111-1111-1111-1111-111111111111@e.com:443#two\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	got := GetExternalLink(srv.URL)
	if got != body {
		t.Errorf("got %q, want the body unchanged", got)
	}
}
