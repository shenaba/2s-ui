package service

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/shenaba/2s-ui/core/proxyheader"
	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"

	M "github.com/sagernet/sing/common/metadata"
)

func TestCheckProxyProtocol(t *testing.T) {
	cases := []struct {
		name    string
		inbound string
		ok      bool
	}{
		{"vless with it", `{"type":"vless","tag":"v","listen":"127.0.0.1","listen_port":10443,"proxy_protocol":true}`, true},
		{"vmess with it", `{"type":"vmess","tag":"v","listen_port":1,"proxy_protocol":true}`, true},
		{"trojan with it", `{"type":"trojan","tag":"t","listen_port":1,"proxy_protocol":true}`, true},
		{"anytls with it", `{"type":"anytls","tag":"a","listen_port":1,"proxy_protocol":true}`, true},
		{"shadowsocks without it", `{"type":"shadowsocks","tag":"s","listen_port":1}`, true},
		{"shadowsocks switched off", `{"type":"shadowsocks","tag":"s","listen_port":1,"proxy_protocol":false}`, true},
		{"shadowsocks with it", `{"type":"shadowsocks","tag":"s","listen_port":1,"proxy_protocol":true}`, false},
		{"hysteria2 with it", `{"type":"hysteria2","tag":"h","listen_port":1,"proxy_protocol":true}`, false},
		{"vless over ws with it", `{"type":"vless","tag":"v","listen_port":1,"proxy_protocol":true,"transport":{"type":"ws"}}`, true},
		{"vless over quic with it", `{"type":"vless","tag":"v","listen_port":1,"proxy_protocol":true,"transport":{"type":"quic"}}`, false},
		{"vless over quic without it", `{"type":"vless","tag":"v","listen_port":1,"transport":{"type":"quic"}}`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var inbound model.Inbound
			if err := inbound.UnmarshalJSON([]byte(c.inbound)); err != nil {
				t.Fatal(err)
			}
			err := checkProxyProtocol(&inbound)
			if c.ok && err != nil {
				t.Fatalf("refused: %v", err)
			}
			if !c.ok && err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

// accept_no_header has nothing to relax and the core ignores it: an inbound
// that arrived with it -- an imported database -- saves, without it.
func TestCheckProxyProtocolDropsAcceptNoHeader(t *testing.T) {
	for _, raw := range []string{
		`{"type":"vless","tag":"v","listen_port":1,"proxy_protocol":true,"proxy_protocol_accept_no_header":true}`,
		`{"type":"vless","tag":"v","listen_port":1,"proxy_protocol_accept_no_header":false}`,
	} {
		var inbound model.Inbound
		if err := inbound.UnmarshalJSON([]byte(raw)); err != nil {
			t.Fatal(err)
		}
		if err := checkProxyProtocol(&inbound); err != nil {
			t.Fatalf("refused %s: %v", raw, err)
		}
		var options map[string]any
		if err := json.Unmarshal(inbound.Options, &options); err != nil {
			t.Fatal(err)
		}
		if _, kept := options["proxy_protocol_accept_no_header"]; kept {
			t.Fatalf("kept accept_no_header in %s", inbound.Options)
		}
		if options["listen_port"] != float64(1) {
			t.Fatalf("lost the rest of the options: %s", inbound.Options)
		}
	}
}

// The trusted fronts are a setting, read by the core per connection: a value
// that does not parse is refused on save, and a saved one reaches the core
// when it is applied (#260).
func TestProxyProtoTrustedReachesTheCore(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "setting.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		proxyheader.SetTrusted(nil)
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	settings := SettingService{}
	db := database.GetDB()
	if _, err := settings.GetAllSetting(); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	if err := settings.Save(db, json.RawMessage(`{"proxyProtoTrusted":"172.18.0.1, not-an-address"}`)); err == nil {
		t.Fatal("an unparsable front was saved")
	}
	// 0.0.0.0/0 would make every address a front and every dial refused.
	for _, broad := range []string{"0.0.0.0/0", "10.0.0.0/8", "172.16.0.0/12", "10.0.0.0/15", "::/0", "2001:db8::/32", "2001:db8::/47", "::ffff:0:0/96", "::ffff:10.0.0.0/111", "64:ff9b::/96", "64:ff9b::a00:0/104", "64:ff9b:1::/96"} {
		if err := settings.Save(db, json.RawMessage(`{"proxyProtoTrusted":"`+broad+`"}`)); err == nil {
			t.Fatalf("saved %s as a trusted front", broad)
		}
	}
	// This host is always trusted; listed, its loopback would also be refused
	// to every client through a relay.
	for _, self := range []string{"127.0.0.1", "127.0.0.0/16", "::1", "0.0.0.0/32", "::/48"} {
		if err := settings.Save(db, json.RawMessage(`{"proxyProtoTrusted":"`+self+`"}`)); err == nil {
			t.Fatalf("saved %s, this host, as a trusted front", self)
		}
	}
	// The narrowest ranges a whole Docker network or site can need still fit.
	for _, wide := range []string{"172.18.0.0/16", "2001:db8::/48", "::ffff:172.18.0.0/112", "64:ff9b::ac12:0/112"} {
		if err := settings.Save(db, json.RawMessage(`{"proxyProtoTrusted":"`+wide+`"}`)); err != nil {
			t.Fatalf("refused %s: %v", wide, err)
		}
	}
	if err := settings.Save(db, json.RawMessage(`{"proxyProtoTrusted":"172.18.0.1"}`)); err != nil {
		t.Fatalf("save: %v", err)
	}
	gateway := M.ParseSocksaddr("172.18.0.1:40000")
	arrived := M.ParseSocksaddr("172.18.0.2:10443")
	if proxyheader.Trusted(gateway, arrived) {
		t.Fatal("trusted before it was applied")
	}
	ApplyProxyProtocol()
	if !proxyheader.Trusted(gateway, arrived) {
		t.Fatal("the saved front did not reach the core")
	}

	// A value that never went through Save -- an imported database, a row
	// written by hand -- is held to the same breadth when it is applied: too
	// broad, it is not used at all, rather than trusting the whole internet.
	for _, stored := range []string{"0.0.0.0/0", "::ffff:0:0/96", "not-an-address"} {
		if err := db.Model(model.Setting{}).Where("key = ?", "proxyProtoTrusted").Update("value", stored).Error; err != nil {
			t.Fatal(err)
		}
		ApplyProxyProtocol()
		if proxyheader.Trusted(M.ParseSocksaddr("203.0.113.9:40000"), arrived) {
			t.Fatalf("a stored %q made a stranger a trusted front", stored)
		}
		if proxyheader.Trusted(gateway, arrived) {
			t.Fatalf("a stored %q kept the fronts of before", stored)
		}
	}
}
