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
		{"accept_no_header on a supported type", `{"type":"vless","tag":"v","listen_port":1,"proxy_protocol":true,"proxy_protocol_accept_no_header":true}`, false},
		{"vless over ws with it", `{"type":"vless","tag":"v","listen_port":1,"proxy_protocol":true,"transport":{"type":"ws"}}`, true},
		{"vless over quic with it", `{"type":"vless","tag":"v","listen_port":1,"proxy_protocol":true,"transport":{"type":"quic"}}`, false},
		{"vless over quic without it", `{"type":"vless","tag":"v","listen_port":1,"transport":{"type":"quic"}}`, true},
		{"accept_no_header alone", `{"type":"vless","tag":"v","listen_port":1,"proxy_protocol_accept_no_header":true}`, false},
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
	for _, broad := range []string{"0.0.0.0/0", "10.0.0.0/7", "::/0", "2001::/31"} {
		if err := settings.Save(db, json.RawMessage(`{"proxyProtoTrusted":"`+broad+`"}`)); err == nil {
			t.Fatalf("saved %s as a trusted front", broad)
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
}
