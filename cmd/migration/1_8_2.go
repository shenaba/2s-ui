package migration

import (
	"encoding/json"
	"strconv"

	"github.com/shenaba/2s-ui/util"

	"gorm.io/gorm"
)

// to1_8_2 repairs the two stored shapes that make sing-box refuse a config it
// was handed, both written by the panel itself.
//
// A vmess share link carries its port as a string, and until 1.8.2 the link
// decoder passed that string on as server_port. The panel's own "convert a
// link" flow stores the result as an outbound row verbatim, so importing a
// vmess link wrote {"server_port":"443"} into outbounds.options -- and
// server_port is a uint16 in sing-box, which fails the *whole* config rather
// than the one outbound. The core then refuses to start and checkCoreJob
// retries the same config every five seconds. The decoder is fixed, but a row
// already written keeps the string, so it is rewritten here.
//
// server_ports is the same failure one step later: sing-box parses each entry
// as a range and refuses a bare port at start-up, not at parse. A single port
// spelled "443" -- what the free-text box in the panel produces for the
// entirely reasonable "443,20000:30000" -- reaches every sing-box subscriber as
// a config that unmarshals and then dies on `bad port range: 443`.
// NormalizePortRanges is the same function FillOutJson now applies on save.
//
// Idempotent: a row already holding the right shapes is left untouched, and a
// value that is not a port at all is dropped rather than guessed at.
func to1_8_2(tx *gorm.DB) error {
	if err := repairOutboundPorts(tx); err != nil {
		return err
	}
	return repairInboundPortRanges(tx)
}

// repairOutboundPorts rewrites a string server_port on an outbound row.
func repairOutboundPorts(tx *gorm.DB) error {
	type outboundRow struct {
		Id      uint
		Options []byte
	}
	var rows []outboundRow
	if err := tx.Raw("SELECT id, options FROM outbounds").Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		options, ok := decodeOptions(row.Options)
		if !ok {
			continue
		}
		port, isString := options["server_port"].(string)
		if !isString {
			continue
		}
		// Left alone unless it is plainly a port. A migration has no business
		// inventing a value for anything else in that field, and dropping the
		// key would change what the outbound points at.
		number, err := strconv.Atoi(port)
		if err != nil || number < 0 || number > 65535 {
			continue
		}
		options["server_port"] = number
		if err := writeOptions(tx, "outbounds", row.Id, options); err != nil {
			return err
		}
	}
	return nil
}

// repairInboundPortRanges normalises server_ports in a stored out_json.
func repairInboundPortRanges(tx *gorm.DB) error {
	type inboundRow struct {
		Id      uint
		OutJson []byte `gorm:"column:out_json"`
	}
	var rows []inboundRow
	if err := tx.Raw("SELECT id, out_json FROM inbounds").Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		outJson, ok := decodeOptions(row.OutJson)
		if !ok {
			continue
		}
		raw, present := outJson["server_ports"]
		if !present {
			continue
		}
		ports := util.NormalizePortRanges(raw)
		if len(ports) == 0 {
			// Nothing usable: carrying it would only fail the subscriber, and
			// the field is optional.
			delete(outJson, "server_ports")
		} else {
			outJson["server_ports"] = ports
		}
		newJson, err := json.Marshal(outJson)
		if err != nil {
			return err
		}
		if err := tx.Exec("UPDATE inbounds SET out_json = ? WHERE id = ?", newJson, row.Id).Error; err != nil {
			return err
		}
	}
	return nil
}

// decodeOptions reads a JSON object column, reporting whether it held one. A
// stored `null` unmarshals into a nil map without an error and writing to that
// would panic, which migrateDb turns into a rolled-back transaction that
// repeats on every start-up.
func decodeOptions(raw []byte) (map[string]interface{}, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var options map[string]interface{}
	if err := json.Unmarshal(raw, &options); err != nil || options == nil {
		return nil, false
	}
	return options, true
}

func writeOptions(tx *gorm.DB, table string, id uint, options map[string]interface{}) error {
	encoded, err := json.Marshal(options)
	if err != nil {
		return err
	}
	return tx.Exec("UPDATE "+table+" SET options = ? WHERE id = ?", encoded, id).Error
}
