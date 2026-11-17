package cronjob

import (
	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service"
)

type DepleteJob struct {
	service.ClientService
	service.InboundService
	service.NodeSyncService
}

func NewDepleteJob() *DepleteJob {
	return new(DepleteJob)
}

func (s *DepleteJob) Run() {
	var enableChanged []string
	var err error
	// Held so that a restart cannot fall between the two: one that read its
	// config before the commit would come up with the depleted clients still
	// enabled, and the hot update below would have skipped the core.
	service.HoldCore(func() {
		// Inbounds over their own cap (#289) first, so the client pass below
		// does not rebuild one that is about to go.
		if _, inbErr := s.InboundService.DepleteInbounds(); inbErr != nil {
			logger.Warning("Disable depleted inbounds failed: ", inbErr)
		}
		var inboundIds []uint
		inboundIds, enableChanged, err = s.ClientService.DepleteClients()
		if err != nil || len(inboundIds) == 0 {
			return
		}
		// UpdateInboundsUsers already filters to node_id IS NULL — only local
		// inbounds are touched here.
		if err := s.InboundService.UpdateInboundsUsers(database.GetDB(), inboundIds); err != nil {
			logger.Error("unable to update inbound users: ", err)
		}
	})
	if err != nil {
		logger.Warning("Disable depleted users failed: ", err)
		return
	}
	// Fan the new enable state out to nodes: reconcile sees it in the expected
	// set and pushes an edit; the node then hot-restarts and drops (or readmits)
	// connections. Both directions have to go — a round that only re-enabled
	// auto-reset clients still leaves the nodes rejecting paid-up users until
	// the hourly safety net.
	if len(enableChanged) > 0 {
		s.NodeSyncService.MarkAllDirty()
		go s.NodeSyncService.ReconcileDirtyOnline()
	}
}
