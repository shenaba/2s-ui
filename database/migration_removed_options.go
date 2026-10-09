package database

import (
	"encoding/json"
	"log"

	"github.com/shenaba/2s-ui/database/model"

	"gorm.io/gorm"
)

// migratedKeyRemovedOptions marks that options sing-box has removed have been
// cleared out of the stored objects.
const migratedKeyRemovedOptions = "migratedRemovedOptions"

// Options sing-box still parses but no longer acts on. They cost nothing to
// leave in place, but the panel no longer offers them, so a stored config would
// keep showing settings the UI cannot explain.
var (
	// Removed in sing-box 1.12.0; the tun stack handles this itself now.
	removedTunOptions = []string{"endpoint_independent_nat"}
	// Removed in sing-box 1.13.0.
	removedECHOptions = []string{"pq_signature_schemes_enabled", "dynamic_record_sizing_disabled"}
)

func migrateRemovedOptions() error {
	var flag model.Setting
	err := db.Where("key = ?", migratedKeyRemovedOptions).First(&flag).Error
	if err == nil {
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}

	return db.Transaction(func(tx *gorm.DB) error {
		changed, err := clearRemovedTunOptions(tx)
		if err != nil {
			return err
		}
		echChanged, err := clearRemovedECHOptions(tx)
		if err != nil {
			return err
		}
		changed += echChanged
		if changed > 0 {
			log.Printf("removed options: cleared %d object(s) of settings sing-box no longer acts on", changed)
		}
		return tx.Create(&model.Setting{Key: migratedKeyRemovedOptions, Value: "true"}).Error
	})
}

func clearRemovedTunOptions(tx *gorm.DB) (int, error) {
	var inbounds []model.Inbound
	if err := tx.Where("type = ?", "tun").Find(&inbounds).Error; err != nil {
		return 0, err
	}
	changed := 0
	for _, inbound := range inbounds {
		options, ok, err := deleteJSONFields(inbound.Options, removedTunOptions)
		if err != nil {
			return 0, err
		}
		if !ok {
			continue
		}
		if err = tx.Model(&model.Inbound{}).Where("id = ?", inbound.Id).
			Update("options", options).Error; err != nil {
			return 0, err
		}
		changed++
	}
	return changed, nil
}

// Only the client side of a TLS config is cleared here; the server side and the
// stored out_json are cleared by migrateRemovedServerECH, which came later.
func clearRemovedECHOptions(tx *gorm.DB) (int, error) {
	var configs []model.Tls
	if err := tx.Find(&configs).Error; err != nil {
		return 0, err
	}
	changed := 0
	for _, tlsConfig := range configs {
		client, ok, err := deleteNestedJSONFields(tlsConfig.Client, "ech", removedECHOptions)
		if err != nil {
			return 0, err
		}
		if !ok {
			continue
		}
		if err = tx.Model(&model.Tls{}).Where("id = ?", tlsConfig.Id).
			Update("client", client).Error; err != nil {
			return 0, err
		}
		changed++
	}
	return changed, nil
}

func deleteJSONFields(raw json.RawMessage, names []string) (json.RawMessage, bool, error) {
	if len(raw) == 0 {
		return raw, false, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return raw, false, nil
	}
	changed := false
	for _, name := range names {
		if _, ok := fields[name]; ok {
			delete(fields, name)
			changed = true
		}
	}
	if !changed {
		return raw, false, nil
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return raw, false, err
	}
	return encoded, true, nil
}

// deleteNestedJSONFields clears fields from one object nested inside raw.
func deleteNestedJSONFields(raw json.RawMessage, key string, names []string) (json.RawMessage, bool, error) {
	if len(raw) == 0 {
		return raw, false, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return raw, false, nil
	}
	nested, ok := fields[key]
	if !ok {
		return raw, false, nil
	}
	cleaned, changed, err := deleteJSONFields(nested, names)
	if err != nil || !changed {
		return raw, false, err
	}
	fields[key] = cleaned
	encoded, err := json.Marshal(fields)
	if err != nil {
		return raw, false, err
	}
	return encoded, true, nil
}

// migratedKeyRemovedServerECH marks the second half of the ECH cleanup. A flag
// of its own because migratedKeyRemovedOptions is already set on every panel
// that ran the first half, which would otherwise never see this one.
const migratedKeyRemovedServerECH = "migratedRemovedServerECH"

// migrateRemovedServerECH finishes what migrateRemovedOptions started for the
// ECH options sing-box removed in 1.13.0. That step cleared only the client
// side of each TLS config, but the options existed on the server side too, and
// the panel copied them from there into every generated client outbound.
//
// Unlike the tun option, these are not inert: sing-box refuses a TLS config
// with either one set to true, on the inbound and on the client alike, so a
// stale true here kept the inbound from starting and broke every subscription
// it fed.
func migrateRemovedServerECH() error {
	var flag model.Setting
	err := db.Where("key = ?", migratedKeyRemovedServerECH).First(&flag).Error
	if err == nil {
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}

	return db.Transaction(func(tx *gorm.DB) error {
		changed, err := clearServerECHOptions(tx)
		if err != nil {
			return err
		}
		outChanged, err := clearOutJsonECHOptions(tx)
		if err != nil {
			return err
		}
		changed += outChanged
		if changed > 0 {
			log.Printf("removed options: cleared legacy ECH options from %d object(s)", changed)
		}
		return tx.Create(&model.Setting{Key: migratedKeyRemovedServerECH, Value: "true"}).Error
	})
}

func clearServerECHOptions(tx *gorm.DB) (int, error) {
	var configs []model.Tls
	if err := tx.Find(&configs).Error; err != nil {
		return 0, err
	}
	changed := 0
	for _, tlsConfig := range configs {
		server, ok, err := deleteNestedJSONFields(tlsConfig.Server, "ech", removedECHOptions)
		if err != nil {
			return 0, err
		}
		if !ok {
			continue
		}
		if err = tx.Model(&model.Tls{}).Where("id = ?", tlsConfig.Id).
			Update("server", server).Error; err != nil {
			return 0, err
		}
		changed++
	}
	return changed, nil
}

// The client outbound each inbound hands to subscriptions is stored, so what
// the panel already copied there stays until the inbound is saved again. Every
// row, not only those with a tls_id: a node replica has its tls_id dropped at
// adoption but keeps the out_json it was given.
func clearOutJsonECHOptions(tx *gorm.DB) (int, error) {
	var inbounds []model.Inbound
	if err := tx.Select("id", "out_json").Find(&inbounds).Error; err != nil {
		return 0, err
	}
	changed := 0
	for _, inbound := range inbounds {
		outJson, ok, err := deleteOutJsonECHOptions(inbound.OutJson)
		if err != nil {
			return 0, err
		}
		if !ok {
			continue
		}
		if err = tx.Model(&model.Inbound{}).Where("id = ?", inbound.Id).
			Update("out_json", outJson).Error; err != nil {
			return 0, err
		}
		changed++
	}
	return changed, nil
}

// deleteOutJsonECHOptions clears the options from tls.ech inside an out_json.
func deleteOutJsonECHOptions(raw json.RawMessage) (json.RawMessage, bool, error) {
	if len(raw) == 0 {
		return raw, false, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return raw, false, nil
	}
	tls, ok := fields["tls"]
	if !ok {
		return raw, false, nil
	}
	cleaned, changed, err := deleteNestedJSONFields(tls, "ech", removedECHOptions)
	if err != nil || !changed {
		return raw, false, err
	}
	fields["tls"] = cleaned
	encoded, err := json.Marshal(fields)
	if err != nil {
		return raw, false, err
	}
	return encoded, true, nil
}
