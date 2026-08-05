package cronjob

import (
	"sync"

	"github.com/shenaba/2s-ui/service"
)

type NodesJob struct {
	service.NodeService
	service.NodeSyncService
	running sync.Mutex
}

func NewNodesJob() *NodesJob {
	return &NodesJob{}
}

func (s *NodesJob) Run() {
	// robfig/cron does not wait for the previous run; with a 5s cadence and a
	// 4s probe timeout a slow batch could overlap itself — skip instead.
	if !s.running.TryLock() {
		return
	}
	defer s.running.Unlock()
	s.NodeService.RefreshAll()
	// Push fresh statuses before the network-bound reconcile below delays them.
	service.HubPushNodesStatus()
	// Offline-period edits converge here: any node that is now online and still
	// dirty gets reconciled (own single-flight + backoff inside).
	s.NodeSyncService.ReconcileDirtyOnline()
}
