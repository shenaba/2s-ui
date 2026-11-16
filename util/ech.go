package util

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/pem"
	"strings"
	"sync"

	"github.com/shenaba/2s-ui/logger"
)

// echConfigPemType is the block sing-box writes for an ECH config; its key goes
// into an "ECH KEYS" block, which must never leave the panel.
const echConfigPemType = "ECH CONFIGS"

// ECHConfigBase64 converts a stored ECH client config into the bare base64
// ECHConfigList that share links (ech=) and mihomo's ech-opts carry, and
// returns "" when there is nothing usable to convert.
//
// The two sides disagree on shape. sing-box holds the PEM text line by line
// (common/tls/ech.go joins the list with newlines and pem.Decodes it, block
// type "ECH CONFIGS"), while links and mihomo want the bare base64 of the
// ECHConfigList with no armor around it.
//
// Decoding the PEM is what makes that robust. Slicing the list positionally --
// everything but the first and last entry -- is right only when it holds
// exactly BEGIN, one body line, END: generateECHKeyPair splits a PEM that ends
// in a newline, so the panel's own output carries a trailing empty entry, and a
// body over 64 characters wraps onto a second line. Both shapes left the END
// marker in the value the client received.
//
// Concatenating every line instead, which upstream changed this to in 1.6.1,
// hands the client the armor in every case; upstream's #1339 skips "-----"
// lines instead, which drops the armor but takes any block at all. Follow
// neither: see the type check below.
//
// The stored value is read through AsStringList rather than asserted: config
// is Listable[string], so a PEM pasted as one string with embedded newlines --
// and anything sing-box itself wrote for a single-entry list -- arrives as a
// bare string.
func ECHConfigBase64(config interface{}) string {
	lines := AsStringList(config)
	if !hasNonBlank(lines) {
		return ""
	}
	// The type is checked, not just the parse. pem.Decode is just as happy with
	// an "ECH KEYS" or "CERTIFICATE" block, and generateECHKeyPair hands the
	// operator the config PEM and the key PEM concatenated into one array while
	// the field itself is a free-text textarea -- so pasting the wrong half
	// would have published the ECH private key to every subscriber, silently.
	text := strings.Join(lines, "\n") + "\n"
	block, _ := pem.Decode([]byte(text))
	if block == nil || block.Type != echConfigPemType {
		warnBadECHConfigOnce(text)
		return ""
	}
	// Standard base64 with padding, which is byte for byte the PEM body.
	return base64.StdEncoding.EncodeToString(block.Bytes)
}

// reportedECHConfigs remembers which bad configs have been complained about,
// by digest so a pasted private key is not kept in memory a second time.
//
// Link generation converts the config once per address per client, so one
// TLS row shared by five hundred clients wrote five hundred identical warnings
// on every links rebuild -- enough to evict whatever the operator was reading
// from the log ring (the same reason warnTlsRowOnce exists). Fixing a config
// and breaking it the same way again reports nothing until a restart.
var reportedECHConfigs sync.Map

func warnBadECHConfigOnce(text string) {
	sum := sha256.Sum256([]byte(text))
	if _, seen := reportedECHConfigs.LoadOrStore(sum, struct{}{}); seen {
		return
	}
	logger.Warning("sub: stored ECH config is not a ", echConfigPemType,
		" PEM block, leaving it out of links and subscriptions")
}

// HasECHConfig reports whether a stored ECH client config holds anything. A
// client with a config uses it and never looks one up, so query_server_name
// only matters while this is false.
func HasECHConfig(config interface{}) bool {
	return hasNonBlank(AsStringList(config))
}

func hasNonBlank(lines []string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			return true
		}
	}
	return false
}

// echFromLink reads a link's ech value back into a sing-box client ECH object:
// either the base64 ECHConfigList, which sing-box takes as a PEM block, or
// Xray's "domain+DNS" form, whose domain is where the config is looked up
// instead (query_server_name). It returns nil for anything else.
//
// The raw value used to be stored as the one line of config, which sing-box
// cannot pem.Decode: every imported ECH link produced an outbound that failed.
func echFromLink(value string) map[string]interface{} {
	// A raw + in the query decodes to a space.
	value = strings.ReplaceAll(strings.TrimSpace(value), " ", "+")
	if value == "" {
		return nil
	}
	// An ECHConfigList is a two-byte length followed by that many bytes.
	if list, err := base64.StdEncoding.DecodeString(value); err == nil &&
		len(list) > 2 && int(list[0])<<8|int(list[1]) == len(list)-2 {
		armored := pem.EncodeToMemory(&pem.Block{Type: echConfigPemType, Bytes: list})
		return map[string]interface{}{
			"enabled": true,
			"config":  strings.Split(strings.TrimSuffix(string(armored), "\n"), "\n"),
		}
	}
	if name, _, found := strings.Cut(value, "+"); found && strings.Contains(name, ".") {
		return map[string]interface{}{
			"enabled":           true,
			"query_server_name": name,
		}
	}
	return nil
}
