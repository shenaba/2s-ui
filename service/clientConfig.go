package service

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/util/common"

	"github.com/gofrs/uuid/v5"
)

// NewClientConfig builds the per-protocol credentials for a new client.
//
// This is the Go counterpart of randomConfigs() in
// frontend/src/types/clients.ts, which is where every client created through
// the panel gets its credentials. The panel had no server-side equivalent
// because nothing but the SPA ever created a client; the Telegram bot is the
// first caller that does.
//
// The two must stay in step -- a protocol added there and not here produces
// clients that work from the panel and silently do not from the bot. The shape
// is a map of protocol name to that protocol's credential fields, which
// ConfigService.Save stores verbatim in Client.Config.
func NewClientConfig(name string) (json.RawMessage, error) {
	shared := common.Random(10)
	ss32, err := randomKey(32)
	if err != nil {
		return nil, err
	}
	ss16, err := randomKey(16)
	if err != nil {
		return nil, err
	}
	id, err := uuid.NewV4()
	if err != nil {
		return nil, err
	}
	uid := id.String()

	cfg := map[string]map[string]any{
		"mixed": {"username": name, "password": shared},
		"socks": {"username": name, "password": shared},
		"http":  {"username": name, "password": shared},
		"naive": {"username": name, "password": shared},
		// alterId is 0 because sing-box only supports VMess AEAD; the field is
		// carried for compatibility with clients that still read it.
		"vmess":  {"name": name, "uuid": uid, "alterId": 0},
		"vless":  {"name": name, "uuid": uid, "flow": "xtls-rprx-vision"},
		"trojan": {"name": name, "password": shared},
		"anytls": {"name": name, "password": shared},
		// Shadowsocks keys are raw bytes in base64, and the length has to match
		// the method -- 16 for the -128- variants, 32 for -256-.
		"shadowsocks":   {"name": name, "password": ss32},
		"shadowsocks16": {"name": name, "password": ss16},
		"shadowtls":     {"name": name, "password": ss32},
		// The listener's psk is shared by everyone on it and lives on the
		// inbound; the userkey is what tells one client from another.
		"snell":     {"name": name, "userkey": common.Random(32)},
		"hysteria":  {"name": name, "auth_str": shared},
		"hysteria2": {"name": name, "password": shared},
		"tuic":      {"name": name, "uuid": uid, "password": shared},
	}
	return json.Marshal(cfg)
}

// randomKey returns n cryptographically random bytes in standard base64,
// matching the frontend's randomShadowsocksPassword.
func randomKey(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		logger.Warning("client config: reading random bytes failed: ", err)
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}

// LocalInboundIDs lists the inbounds a new client should be attached to: every
// inbound this panel runs itself. Node replicas are excluded -- attaching one
// here would be attaching a client to an inbound that lives on another panel.
func LocalInboundIDs() ([]uint, error) {
	var ids []uint
	err := database.GetDB().Table("inbounds").Where("node_id IS NULL").Pluck("id", &ids).Error
	return ids, err
}
