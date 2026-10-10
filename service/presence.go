package service

import (
	"sort"
	"sync"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service/notify"
)

// presenceTracker turns the online list into client.online / client.offline
// notifications.
//
// The online list is not a presence signal on its own: a client is "online"
// for one 10s flush when it moved traffic in it, so an idle phone flips off and
// back on all day, and every core restart takes everyone off for a few
// seconds. Notify's Suppressor keeps nothing that could absorb that (see its
// comment), so the debounce lives here: a client is only declared offline once
// it has been absent for the whole grace period, and only a client that was
// declared offline can come online again. Anything shorter than the grace
// produces no message at all.
//
// The same grace covers start-up. Clients reconnect over the first seconds of
// a fresh process, and announcing each of them would greet every panel restart
// with the whole client list; for one grace period after the first
// observation, clients that turn up are recorded as online silently.
type presenceTracker struct {
	mu        sync.Mutex
	startedAt time.Time
	// online is the declared state: present means online.
	online map[string]time.Time // name -> last time it was in the list
}

// observe folds one flush into the declared state and reports the clients
// whose declared state changed.
func (p *presenceTracker) observe(now time.Time, present []string, grace time.Duration) (cameOnline, wentOffline []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.online == nil {
		p.online = make(map[string]time.Time)
		p.startedAt = now
	}
	warm := now.Sub(p.startedAt) >= grace

	for _, name := range present {
		if _, known := p.online[name]; !known && warm {
			cameOnline = append(cameOnline, name)
		}
		p.online[name] = now
	}
	for name, lastSeen := range p.online {
		if now.Sub(lastSeen) >= grace {
			delete(p.online, name)
			wentOffline = append(wentOffline, name)
		}
	}
	sort.Strings(cameOnline)
	sort.Strings(wentOffline)
	return cameOnline, wentOffline
}

// reset forgets every client, so the next observe starts a fresh start-up
// grace.
func (p *presenceTracker) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.online = nil
}

var presence presenceTracker

// presenceGraceDefault is used when notifyPresenceGrace is unreadable or below
// one minute. Zero cannot mean "off" here the way it does for the thresholds:
// without a grace every idle client would flap, so the toggle is the off switch.
const presenceGraceDefault = 5 * time.Minute

// ObservePresence runs after every stats flush.
//
// With these events switched off it reads the notify settings once and stops:
// the online list (a client-table scan once nodes report users) is not read,
// and the state is dropped rather than advanced. Turning the events on later
// then starts from a fresh tracker, whose start-up grace records the clients
// already online silently -- the same reason a panel restart announces nobody.
func ObservePresence() {
	var settingService SettingService
	m := settingService.notifySettings()
	if !notifyWantsFrom(m, notify.ClientOnline, notify.ClientOffline) {
		presence.reset()
		return
	}

	var stats StatsService
	current, err := stats.GetClusterOnlines()
	if err != nil {
		logger.Warning("presence: read onlines: ", err)
		return
	}

	grace := time.Duration(notifyThresholdsFrom(m).PresenceGrace) * time.Minute
	if grace < time.Minute {
		grace = presenceGraceDefault
	}
	cameOnline, wentOffline := presence.observe(time.Now(), current.User, grace)
	if len(cameOnline) == 0 && len(wentOffline) == 0 {
		return
	}
	// A client deleted while online drops out of the list like one that left,
	// and is not worth a message.
	wentOffline = existingClients(wentOffline)

	// One event per flush and direction, not per client: a node coming back
	// brings all of its clients with it, and that is one thing that happened.
	if len(cameOnline) > 0 {
		notify.Publish(notify.Event{
			Kind:    notify.ClientOnline,
			Subject: "batch",
			Data:    &notify.ClientData{Names: cameOnline},
		})
	}
	if len(wentOffline) > 0 {
		notify.Publish(notify.Event{
			Kind:    notify.ClientOffline,
			Subject: "batch",
			Data:    &notify.ClientData{Names: wentOffline},
		})
	}
}

func existingClients(names []string) []string {
	if len(names) == 0 {
		return names
	}
	var kept []string
	if err := database.GetDB().Model(model.Client{}).Where("name IN ?", names).
		Order("name").Pluck("name", &kept).Error; err != nil {
		logger.Warning("presence: read clients: ", err)
		return names
	}
	return kept
}
