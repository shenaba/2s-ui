package util

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func linkQuery(t *testing.T, link string) url.Values {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse %q: %v", link, err)
	}
	return u.Query()
}

func vmessObj(t *testing.T, link string) map[string]interface{} {
	t.Helper()
	raw, err := B64StrToByte(strings.TrimPrefix(link, "vmess://"))
	if err != nil {
		t.Fatalf("decoding %q: %v", link, err)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("unmarshalling %q: %v", raw, err)
	}
	return obj
}

// With no Host of its own a ws or httpupgrade link carries the SNI as its
// Host: left out, the client sends the server address, which a CDN reached
// through a clean-IP domain answers with 403 (upstream #1334).
func TestLinksFallBackToSniForTransportHost(t *testing.T) {
	tlsOn := map[string]interface{}{"enabled": true, "server_name": "cdn.example.com"}
	tlsOff := map[string]interface{}{"enabled": false, "server_name": "cdn.example.com"}
	addr := func(tls map[string]interface{}) []map[string]interface{} {
		return []map[string]interface{}{{
			"server": "1.2.3.4", "server_port": float64(443), "remark": "r", "tls": tls,
		}}
	}
	cases := []struct {
		name      string
		transport map[string]interface{}
		tls       map[string]interface{}
		want      string
	}{
		{"ws without host", map[string]interface{}{"type": "ws", "path": "/ws"}, tlsOn, "cdn.example.com"},
		{"httpupgrade without host", map[string]interface{}{"type": "httpupgrade", "path": "/u"}, tlsOn, "cdn.example.com"},
		{"ws with empty host", map[string]interface{}{"type": "ws", "headers": map[string]interface{}{"Host": ""}}, tlsOn, "cdn.example.com"},
		{"ws keeps its own host", map[string]interface{}{"type": "ws", "headers": map[string]interface{}{"Host": "own.example.com"}}, tlsOn, "own.example.com"},
		{"ws host as a list", map[string]interface{}{"type": "ws", "headers": map[string]interface{}{"Host": []interface{}{"list.example.com"}}}, tlsOn, "list.example.com"},
		{"httpupgrade keeps its own host", map[string]interface{}{"type": "httpupgrade", "host": "own.example.com"}, tlsOn, "own.example.com"},
		{"no TLS, no fallback", map[string]interface{}{"type": "ws"}, tlsOff, ""},
		{"grpc is left alone", map[string]interface{}{"type": "grpc", "service_name": "s"}, tlsOn, ""},
	}
	for _, tc := range cases {
		inbound := map[string]interface{}{"transport": tc.transport}
		for _, link := range append(
			vlessLink(map[string]interface{}{"uuid": "u"}, inbound, addr(tc.tls)),
			trojanLink(map[string]interface{}{"password": "p"}, inbound, addr(tc.tls))...,
		) {
			q := linkQuery(t, link)
			if got := q.Get("host"); got != tc.want {
				t.Errorf("%s: %s host = %q, want %q", tc.name, link, got, tc.want)
			}
			if len(q["host"]) > 1 {
				t.Errorf("%s: %s carries host twice", tc.name, link)
			}
			if _, ok := q["host"]; ok && tc.want == "" {
				t.Errorf("%s: %s carries an empty host param", tc.name, link)
			}
		}
		links := vmessLink(map[string]interface{}{"uuid": "u"}, inbound, addr(tc.tls))
		got, _ := vmessObj(t, links[0])["host"].(string)
		if got != tc.want {
			t.Errorf("%s: vmess host = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Every address of an inbound shares the one transport map.
func TestSetTransportHostCopies(t *testing.T) {
	ws := map[string]interface{}{"type": "ws", "headers": map[string]interface{}{"X-Other": "1"}}
	got := SetTransportHost(ws, "a.example.com")
	if _, ok := ws["headers"].(map[string]interface{})["Host"]; ok {
		t.Fatal("SetTransportHost wrote into the shared headers")
	}
	headers := got["headers"].(map[string]interface{})
	if headers["Host"] != "a.example.com" || headers["X-Other"] != "1" {
		t.Errorf("headers = %v", headers)
	}
	hu := SetTransportHost(map[string]interface{}{"type": "httpupgrade"}, "b.example.com")
	if hu["host"] != "b.example.com" || hu["headers"] != nil {
		t.Errorf("httpupgrade = %v", hu)
	}
}
