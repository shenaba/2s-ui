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
	inboundIds, enableChanged, err := s.ClientService.DepleteClients()
	if err != nil {
		logger.Warning("Disable depleted users failed: ", err)
		return
	}
	if len(inboundIds) > 0 {
		// UpdateInboundsUsers already filters to node_id IS NULL — only local
		// inbounds are touched here.
		err := s.InboundService.UpdateInboundsUsers(database.GetDB(), inboundIds)
		if err != nil {
			logger.Error("unable to update inbound users: ", err)
		}
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
