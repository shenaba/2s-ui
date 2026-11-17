package model

import (
	"encoding/json"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

type Inbound struct {
	Id   uint   `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Type string `json:"type" form:"type"`
	Tag  string `json:"tag" form:"tag" gorm:"unique"`

	// Enable false keeps the inbound -- its row, its tag, its clients -- but
	// leaves it out of the core, and its links out of every subscription.
	// The default is what an existing row gets when the column is added, and
	// what an Inbound literal that never says gets on create.
	Enable bool `json:"enable" form:"enable" gorm:"default:true;not null"`
	// EnableSet reports that Enable was given on purpose: the decoded request
	// named it, or the code that built the struct said so. An edit that does
	// not -- an older apiv2 caller, a drawer that rebuilt the object -- keeps
	// the stored value rather than switching the inbound back on
	// (InboundService.Save), and a create stores false only when it is set.
	EnableSet bool `json:"-" form:"-" gorm:"-"`
	// createDisabled carries a false Enable across an INSERT; see BeforeCreate.
	createDisabled bool

	// Foreign key to tls table
	TlsId uint `json:"tls_id" form:"tls_id"`
	Tls   *Tls `json:"tls" form:"tls" gorm:"foreignKey:TlsId;references:Id"`

	// NULL = this inbound runs on the local core; set = it is a replica of an
	// inbound living on that managed node and must NEVER reach the local core
	// (see the node_id IS NULL guards in service/inbounds.go).
	NodeId *uint `json:"node_id,omitempty" form:"node_id" gorm:"index"`

	Addrs   json.RawMessage `json:"addrs" form:"addrs"`
	OutJson json.RawMessage `json:"out_json" form:"out_json"`
	Options json.RawMessage `json:"-" form:"-"`

	// Traffic cap in bytes over Up+Down; 0 = unlimited (#289). DepleteJob
	// switches the inbound off (Enable) once it is spent. Local inbounds only:
	// a replica's traffic is counted on its node, which can cap it there.
	Volume int64 `json:"volume" form:"volume" gorm:"default:0;not null"`
	// VolumeSet reports that the decoded request named volume. An edit that
	// does not keeps the stored cap, as for Enable -- an older apiv2 caller
	// would otherwise lift it with an unrelated edit.
	VolumeSet bool `json:"-" form:"-" gorm:"-"`

	// Bytes counted on this inbound, maintained by the stats flush alone --
	// no save writes them (InboundService.Save omits both columns), so an edit
	// from a form loaded a minute ago cannot write an older count back. A
	// counter on the row rather than a sum over the stats table, which is
	// neither written with traffic history off nor kept past its retention.
	Up   int64 `json:"up" form:"up" gorm:"default:0;not null"`
	Down int64 `json:"down" form:"down" gorm:"default:0;not null"`
}

// Depleted reports whether the inbound has spent its cap. The same comparison
// as a client's (up+down > volume), so the two read alike in the panel.
func (i *Inbound) Depleted() bool {
	return i.Volume > 0 && i.Up+i.Down > i.Volume
}

func (i *Inbound) UnmarshalJSON(data []byte) error {
	var err error
	var raw map[string]interface{}
	if err = json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Extract fixed fields and store the rest in Options
	if val, exists := raw["id"].(float64); exists {
		i.Id = uint(val)
	}
	delete(raw, "id")
	i.Type, _ = raw["type"].(string)
	delete(raw, "type")
	i.Tag, _ = raw["tag"].(string)
	delete(raw, "tag")

	// Enable. Absent means enabled for a new inbound; Save keeps the stored
	// value for an edit. A script's "true" or 1 is taken as meant; anything
	// that says neither on nor off is refused rather than guessed at. Taken
	// out of raw either way: sing-box rejects the field.
	i.Enable, i.EnableSet = true, false
	if val, exists := raw["enable"]; exists && val != nil {
		enable, ok := parseEnable(val)
		if !ok {
			return fmt.Errorf("inbound %q: enable must be true or false, got %v", i.Tag, val)
		}
		i.Enable, i.EnableSet = enable, true
	}
	delete(raw, "enable")

	// TlsId
	if val, exists := raw["tls_id"].(float64); exists {
		i.TlsId = uint(val)
	}
	delete(raw, "tls_id")
	delete(raw, "tls")
	delete(raw, "users")

	// NodeId — must be pulled out of raw or it would land in Options and be
	// fed to sing-box, which rejects unknown fields.
	if val, exists := raw["node_id"].(float64); exists && val > 0 {
		nodeId := uint(val)
		i.NodeId = &nodeId
	}
	delete(raw, "node_id")

	// Addrs
	i.Addrs, _ = json.MarshalIndent(raw["addrs"], "", "  ")
	delete(raw, "addrs")

	// OutJson
	i.OutJson, _ = json.MarshalIndent(raw["out_json"], "", "  ")
	delete(raw, "out_json")

	// The traffic cap; up/down are the stats flush's to write and are dropped
	// whatever they hold. All three are panel fields, which sing-box refuses.
	i.Volume, i.VolumeSet = 0, false
	if val, exists := raw["volume"].(float64); exists {
		if val > 0 {
			i.Volume = int64(val)
		}
		i.VolumeSet = true
	}
	delete(raw, "volume")
	delete(raw, "up")
	delete(raw, "down")

	// Remaining fields
	i.Options, err = json.MarshalIndent(raw, "", "  ")
	return err
}

// MarshalJSON customizes marshalling
func (i Inbound) MarshalJSON() ([]byte, error) {
	// Combine fixed fields and dynamic fields into one map
	combined := make(map[string]interface{})
	combined["type"] = i.Type
	combined["tag"] = i.Tag
	if i.Tls != nil {
		combined["tls"] = i.Tls.Server
	}

	if i.Options != nil {
		var restFields map[string]json.RawMessage
		if err := json.Unmarshal(i.Options, &restFields); err != nil {
			return nil, err
		}

		for k, v := range restFields {
			combined[k] = v
		}
	}

	return json.Marshal(combined)
}

func (i Inbound) MarshalFull() (*map[string]interface{}, error) {
	combined := make(map[string]interface{})
	combined["id"] = i.Id
	combined["type"] = i.Type
	combined["tag"] = i.Tag
	combined["enable"] = i.Enable
	combined["tls_id"] = i.TlsId
	combined["addrs"] = i.Addrs
	combined["out_json"] = i.OutJson
	combined["volume"] = i.Volume
	combined["up"] = i.Up
	combined["down"] = i.Down
	// Panel shape only — MarshalJSON (sing-box shape) must never carry this.
	if i.NodeId != nil {
		combined["node_id"] = *i.NodeId
	}

	if i.Options != nil {
		var restFields map[string]interface{}
		if err := json.Unmarshal(i.Options, &restFields); err != nil {
			return nil, err
		}

		for k, v := range restFields {
			combined[k] = v
		}
	}
	return &combined, nil
}

// BeforeCreate and AfterCreate keep a deliberate false Enable through an
// INSERT. GORM leaves a zero value out of the INSERT for a column that has a
// default, so creating a disabled inbound -- a new one saved off, a disabled
// one cloned, a node's disabled inbound adopted -- stored it enabled, and read
// the default back into the struct as well.
//
// Only a deliberate one (EnableSet): false is also the zero value, and every
// literal that never mentions enable -- a migration's, a test fixture's -- is
// meant to get the column's default.
func (i *Inbound) BeforeCreate(tx *gorm.DB) error {
	i.createDisabled = i.EnableSet && !i.Enable
	return nil
}

func (i *Inbound) AfterCreate(tx *gorm.DB) error {
	if !i.createDisabled {
		return nil
	}
	i.createDisabled = false
	i.Enable = false
	return tx.Model(&Inbound{}).Where("id = ?", i.Id).UpdateColumn("enable", false).Error
}

// parseEnable reads an enable flag in the shapes API callers send it: a JSON
// bool, the strings "true"/"false" (any case), or the numbers 1/0. Anything
// else reports false in ok.
func parseEnable(val interface{}) (enable bool, ok bool) {
	switch v := val.(type) {
	case bool:
		return v, true
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1":
			return true, true
		case "false", "0":
			return false, true
		}
	case float64:
		switch v {
		case 1:
			return true, true
		case 0:
			return false, true
		}
	}
	return false, false
}
