package service

import (
	"encoding/json"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service/notify"
	"github.com/shenaba/2s-ui/util/common"

	"gorm.io/gorm"
)

// Per-inbound traffic caps (#289). The counters are the row's own up/down,
// which the stats flush advances; the cap is judged here, once a minute, by
// DepleteJob -- the same cadence and the same comparison as a client's quota.
//
// A spent inbound is switched off with the inbound's own switch (Enable), so
// it ends up exactly where switching it off by hand leaves it: out of the core,
// its connections closed, its links out of every subscription. The one
// difference is the detour checks a save makes. A save refuses to switch off
// an inbound that an enabled listener hands its connections to, or that an
// enabled client config rides, because that breaks the rider without saying
// so. The cap cannot be refused -- the traffic is spent -- so it switches the
// inbound off regardless, and the chain behind it stops serving with it, which
// is what a spent cap means; the notification says which inbound it was.

// depletedInboundsWhere selects the local, enabled inbounds over their cap.
const depletedInboundsWhere = "enable = ? AND node_id IS NULL AND volume > 0 AND up + down > volume"

// DepleteInbounds switches off every local inbound over its cap and takes it
// out of the running core. Returns the tags it switched off.
//
// The caller holds the core's read side (HoldCore), as DepleteJob does for the
// clients: a restart that read its config before this commit would otherwise
// bring the inbound straight back.
func (s *InboundService) DepleteInbounds() ([]string, error) {
	db := database.GetDB()
	dt := time.Now().Unix()

	var tags []string
	err := db.Transaction(func(tx *gorm.DB) error {
		var ids []uint
		if err := tx.Model(model.Inbound{}).Where(depletedInboundsWhere, true).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		// Written first, which takes the write lock: the link rebuild below
		// reads and rewrites whole client rows, and must not interleave with a
		// stats flush adding to their counters.
		if err := tx.Model(model.Inbound{}).Where("id IN ?", ids).Update("enable", false).Error; err != nil {
			return err
		}
		var spent []model.Inbound
		if err := tx.Model(model.Inbound{}).Preload("Tls").Where("id IN ?", ids).Find(&spent).Error; err != nil {
			return err
		}
		// Off, an inbound has no links (inboundLinks), so this only takes
		// them out; no hostname is needed to build none.
		if err := s.ClientService.UpdateLinksByInboundChange(tx, &spent, "", ""); err != nil {
			return err
		}
		changes := make([]model.Changes, 0, len(spent))
		for _, inb := range spent {
			tags = append(tags, inb.Tag)
			changes = append(changes, model.Changes{
				DateTime: dt,
				Actor:    "DepleteJob",
				Key:      "inbounds",
				Action:   "disable",
				Obj:      changeObjName(inb.Tag),
			})
		}
		return tx.Create(&changes).Error
	})
	if err != nil {
		return nil, err
	}
	if len(tags) == 0 {
		return nil, nil
	}

	// After the commit, as a save switching it off does (see Save): the row
	// already says off, so a removal that fails only leaves it running until
	// the next restart builds the config without it.
	if corePtr != nil {
		for _, tag := range tags {
			removeFromCore(tag)()
		}
	}
	// After the core, too: the push is what shows the inbound as spent, and a
	// reset sent from that view before the removal above had run would be
	// undone by it.
	SetLastUpdate(dt)
	logger.Info("switched off inbounds out of traffic: ", tags)
	publishInboundDepleted(tags)
	return tags, nil
}

func publishInboundDepleted(tags []string) {
	var settingService SettingService
	if !settingService.NotifyWants(notify.InboundDepleted) {
		return
	}
	notify.Publish(notify.Event{
		Kind:    notify.InboundDepleted,
		Subject: "batch",
		Data:    &notify.InboundData{Tags: tags},
	})
}

// resetTraffic zeroes one local inbound's counters, the "resetTraffic" action
// of an inbounds save. data is the tag, as for "del".
//
// An inbound the cap had switched off -- off and over its cap until now -- is
// switched back on, as a save switching it on would: its own detours checked,
// started in the core, its links back. One the operator switched off with
// traffic to spare stays off: nothing about a reset says they changed their
// mind.
func (s *InboundService) resetTraffic(tx *gorm.DB, data json.RawMessage, hostname string) error {
	var tag string
	if err := json.Unmarshal(data, &tag); err != nil {
		return err
	}
	var inbound model.Inbound
	err := tx.Model(model.Inbound{}).Preload("Tls").Where("tag = ?", tag).Limit(1).Find(&inbound).Error
	if err != nil {
		return err
	}
	if inbound.Id == 0 {
		return common.NewErrorf("inbound %s not found", tag)
	}
	if inbound.NodeId != nil {
		return common.NewError("inbound belongs to a node: manage it on the node panel")
	}
	reenable := !inbound.Enable && inbound.Depleted()
	if reenable {
		// The checks a save switching it on makes: an inbound whose own
		// detour is off would come back unable to serve.
		inbound.Enable = true
		if err = checkListenDetour(tx, &inbound, inbound.Tag); err != nil {
			return err
		}
		if err = checkClientDetour(tx, &inbound, inbound.Tag); err != nil {
			return err
		}
	}
	update := map[string]interface{}{"up": 0, "down": 0}
	if reenable {
		update["enable"] = true
	}
	err = tx.Model(model.Inbound{}).Where("id = ?", inbound.Id).Updates(update).Error
	if err != nil {
		return err
	}
	if !reenable {
		return nil
	}
	// Reads the row inside this transaction, where it is on now. Inside, as a
	// save adds one: a listener that cannot start fails the reset.
	if err = s.RestartInbounds(tx, []uint{inbound.Id}); err != nil {
		return err
	}
	return s.ClientService.UpdateLinksByInboundChange(tx, &[]model.Inbound{inbound}, hostname, inbound.Tag)
}
