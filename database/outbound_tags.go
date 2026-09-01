package database

import (
	"encoding/json"

	"github.com/shenaba/2s-ui/database/model"

	"gorm.io/gorm"
)

// plainDirectOutboundTags collects the outbounds a detour must not name.
//
// sing-box refuses a detour to a direct outbound carrying no options of its
// own, so the two migrations that rewrite a rule-set's download detour have to
// drop it in exactly that case -- and only in it. Matching on the literal tag
// "direct" instead would also drop a detour to a direct outbound that does
// carry options (bind_interface, domain_resolver, routing_mark), silently
// taking rule-set downloads off the interface the operator put them on.
//
// This is the same rule the TLS/ruleset forms apply on the frontend, in
// plugins/httpClient.ts isNoopDetour, and the same one util.IsPlainDirectOutbound
// applies to an outbound already rendered into sing-box shape.
func plainDirectOutboundTags(tx *gorm.DB) (map[string]struct{}, error) {
	var outbounds []model.Outbound
	if err := tx.Where("type = ?", "direct").Find(&outbounds).Error; err != nil {
		return nil, err
	}
	tags := make(map[string]struct{}, len(outbounds))
	for _, outbound := range outbounds {
		if outbound.Tag == "" {
			continue
		}
		// "Carries no options" is exactly "the Options blob holds nothing", so
		// ask it directly. Rendering the row and counting the keys that came
		// back would answer the same question through three JSON passes, and
		// would inherit MarshalJSON's own quirk of dropping a disabled tls
		// block -- which would report an outbound that does carry options as a
		// no-op and strip a detour that should have stayed.
		var options map[string]json.RawMessage
		if len(outbound.Options) > 0 {
			if err := json.Unmarshal(outbound.Options, &options); err != nil {
				// Options we cannot read are not options we can claim are
				// absent, so the detour to this outbound is left alone.
				continue
			}
		}
		if len(options) == 0 {
			tags[outbound.Tag] = struct{}{}
		}
	}
	return tags, nil
}
