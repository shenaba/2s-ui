package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// The hub's decisions are factored into pure helpers for the same reason
// acme_test.go's are: they can then be verified without a database, a running
// core, or a live socket.

func TestMergeResources(t *testing.T) {
	tests := []struct {
		name  string
		lists [][]string
		want  []string
	}{
		// One sample per tick serves every subscriber, so the sampled set is the
		// union — a tab with the resources tile closed must not shrink what a
		// tab with it open receives.
		{
			name:  "union across subscribers",
			lists: [][]string{{"net", "sbd"}, {"net", "sbd", "cpu", "mem"}},
			want:  []string{"net", "sbd", "cpu", "mem"},
		},
		// Duplicates would make ServerService sample the same resource twice.
		{
			name:  "dedupes",
			lists: [][]string{{"net", "net"}, {"net"}},
			want:  []string{"net"},
		},
		// First-seen order keeps the request string stable across ticks, which
		// keeps the sampled key set deterministic.
		{
			name:  "preserves first-seen order",
			lists: [][]string{{"cpu", "net"}, {"mem", "cpu"}},
			want:  []string{"cpu", "net", "mem"},
		},
		{name: "no subscribers", lists: nil, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mergeResources(tt.lists); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("mergeResources(%v) = %v, want %v", tt.lists, got, tt.want)
			}
		})
	}
}

func TestShouldPushNodes(t *testing.T) {
	// Empty→empty is the zero-node panel: pushing every 5s forever would cost
	// every open tab a message for news that never changes.
	if shouldPushNodes(0, 0) {
		t.Error("empty -> empty should not push")
	}
	// The transition to empty MUST go out, or the last deleted node keeps its
	// badge: the client reads a missing/unchanged nodesStatus as "no news".
	if !shouldPushNodes(0, 2) {
		t.Error("last node deleted must push one final empty map")
	}
	if !shouldPushNodes(1, 0) {
		t.Error("first node appearing must push")
	}
	if !shouldPushNodes(3, 3) {
		t.Error("same count must still push — the states inside changed")
	}
}

func TestSeedNodesStatus(t *testing.T) {
	// A full payload omits the key when there are no nodes, but the client
	// treats a missing key as "unchanged" — so full payloads must say "none"
	// explicitly or a deletion never clears on screen.
	data := map[string]interface{}{"config": "x"}
	seedNodesStatus(data)
	got, ok := data["nodesStatus"]
	if !ok {
		t.Fatal("seedNodesStatus must add the key when absent")
	}
	if m, isMap := got.(map[uint]NodeStatus); !isMap || len(m) != 0 {
		t.Errorf("want an empty map, got %#v", got)
	}

	// An existing value must survive untouched.
	live := map[uint]NodeStatus{1: {State: "online"}}
	data = map[string]interface{}{"nodesStatus": live}
	seedNodesStatus(data)
	if !reflect.DeepEqual(data["nodesStatus"], live) {
		t.Errorf("seedNodesStatus must not overwrite live statuses: %#v", data["nodesStatus"])
	}
}

func TestStatsEnvelopeEchoesKey(t *testing.T) {
	// The key is echoed so a client can drop a push that raced a period switch;
	// without it a slow answer for "hour" would render under "90day" labels.
	key := statsSubKey{Resource: "client", Tag: "u1", Period: "hour"}
	var env struct {
		Topic string `json:"topic"`
		Data  struct {
			Resource string        `json:"resource"`
			Tag      string        `json:"tag"`
			Period   string        `json:"period"`
			Stats    []interface{} `json:"stats"`
		} `json:"data"`
	}
	if err := json.Unmarshal(statsEnvelope(key, nil), &env); err != nil {
		t.Fatalf("statsEnvelope produced invalid JSON: %v", err)
	}
	if env.Topic != "stats" {
		t.Errorf("topic = %q, want stats", env.Topic)
	}
	if env.Data.Resource != key.Resource || env.Data.Tag != key.Tag || env.Data.Period != key.Period {
		t.Errorf("key not echoed back: %+v", env.Data)
	}
	// A failed query answers with no rows rather than staying silent, so the
	// modal can render its empty state instead of a chart that never fills.
	if env.Data.Stats != nil {
		t.Errorf("nil rows should serialize as null, got %#v", env.Data.Stats)
	}
}

func TestHubClientAllowQuery(t *testing.T) {
	// Read limits bound message size, not rate; this is what stops a client
	// looping subscribe frames from driving unbounded database work.
	c := &hubClient{}
	now := time.Now()
	if !c.allowQuery("load", now) {
		t.Fatal("first query must be allowed")
	}
	if c.allowQuery("load", now.Add(minQueryInterval/2)) {
		t.Error("a second query inside the window must be refused")
	}
	if !c.allowQuery("load", now.Add(minQueryInterval*2)) {
		t.Error("a query past the window must be allowed again")
	}
	// Budgets are per topic: a reconnect re-sends every subscription at once,
	// and one topic must not starve the next.
	if !c.allowQuery("stats", now.Add(minQueryInterval*2)) {
		t.Error("a different topic must have its own budget")
	}
}

func TestStartStopHubIsIdempotent(t *testing.T) {
	// StopHub runs on every RestartApp (SIGHUP / api/restartApp); it must leave
	// no hub behind and must not deadlock waiting on its own goroutines.
	if getHub() != nil {
		t.Fatal("test started with a hub already running")
	}
	StartHub()
	h := getHub()
	if h == nil {
		t.Fatal("StartHub did not install a hub")
	}
	StartHub() // second call must not replace the singleton
	if getHub() != h {
		t.Error("StartHub must be idempotent")
	}

	// Every notify entry point must be safe with no clients attached.
	NotifyConfigChanged()
	HubPushNodesStatus()

	done := make(chan struct{})
	go func() { StopHub(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("StopHub did not return — goroutine leak or deadlock")
	}
	if getHub() != nil {
		t.Error("StopHub must clear the singleton")
	}
	// After teardown the notify hooks are still reachable from in-flight cron
	// jobs (cron.Stop does not wait for them) and must no-op rather than panic.
	NotifyConfigChanged()
	HubAfterStatsFlush()
	HubPushNodesStatus()
}

func TestConfigCacheUsable(t *testing.T) {
	const host = "panel.example"
	fresh := configCacheTTL / 2

	if configCacheUsable(false, host, host, 7, 7, fresh) {
		t.Error("an empty cache must never be served")
	}
	if !configCacheUsable(true, host, host, 7, 7, fresh) {
		t.Error("same host, same LastUpdate, inside the TTL: should be served")
	}
	// subURI is derived from the hostname, so an entry built for one host would
	// hand the wrong subscription link to another.
	if configCacheUsable(true, host, "other.example", 7, 7, fresh) {
		t.Error("a different hostname must rebuild")
	}
	// The load-bearing one: any write bumps LastUpdate, and missing this would
	// serve the pre-change config to every reconnect for the whole TTL.
	if configCacheUsable(true, host, host, 7, 8, fresh) {
		t.Error("a bumped LastUpdate must rebuild")
	}
	// Backstop for a write path that forgets to bump: staleness stays bounded
	// by the TTL instead of lasting until the next real change.
	if configCacheUsable(true, host, host, 7, 7, configCacheTTL) {
		t.Error("an entry at the TTL boundary must rebuild")
	}
}

func TestConfigSeqSeededAboveZero(t *testing.T) {
	// Clients ignore a config payload whose version is not above the one they
	// applied. A counter starting at zero would make everything after a restart
	// look stale to an already-open tab, and it would ignore every push until
	// the counter climbed past its stored value.
	if got := configSeq.Load(); got == 0 {
		t.Fatal("configSeq must be seeded, not start at zero")
	}
	first := configSeq.Add(1)
	if second := configSeq.Add(1); second <= first {
		t.Errorf("configSeq must increase: %d then %d", first, second)
	}
}

// "cpu" throughout the tests below: the default resource set includes "sbd",
// which reaches corePtr — nil without a running core.
const testStatusParams = `{"r":["cpu"]}`

func TestStatusLoopStartsAndStopsWithSubscribers(t *testing.T) {
	// The 2s gopsutil sampler must exist only while somebody watches: it is the
	// one piece of periodic work an idle panel would otherwise keep paying for.
	StartHub()
	defer StopHub()
	h := getHub()

	c := &hubClient{send: make(chan []byte, hubSendBuffer), closed: make(chan struct{})}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	// Runs before StopHub (LIFO): this client has no conn, and StopHub would
	// dereference it.
	defer func() {
		h.mu.Lock()
		delete(h.clients, c)
		h.mu.Unlock()
	}()

	running := func() bool {
		h.statusMu.Lock()
		defer h.statusMu.Unlock()
		return h.statusStop != nil
	}

	if running() {
		t.Fatal("sampler must not run before anyone subscribes")
	}

	h.subscribe(c, "status", json.RawMessage(testStatusParams))
	if !running() {
		t.Error("subscribing to status must start the sampler")
	}
	// Subscribes are answered immediately; waiting a full tick is what the
	// ticker alone would do.
	select {
	case msg := <-c.send:
		var env struct {
			Topic string                 `json:"topic"`
			Data  map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(msg, &env); err != nil {
			t.Fatalf("subscribe answer is not valid JSON: %v", err)
		}
		if env.Topic != "status" {
			t.Errorf("topic = %q, want status", env.Topic)
		}
		if _, ok := env.Data["t"]; !ok {
			t.Error("sample must carry the server timestamp clients derive rates from")
		}
	default:
		t.Error("subscribe must answer immediately, not wait for the first tick")
	}

	h.unsubscribe(c, "status")
	if running() {
		t.Error("the last unsubscribe must stop the sampler")
	}
}

func TestHubServeSubscribeAndDisconnect(t *testing.T) {
	// Covers what the pure helpers cannot: HubServe registering a real
	// connection, the read pump decoding a frame, the write pump delivering the
	// answer, and the drop path deregistering on disconnect. A leak here means
	// every closed tab keeps a goroutine pair for the panel's lifetime.
	StartHub()
	defer StopHub()
	h := getHub()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Origin checking is WsHandler's business, not the hub's.
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		// Zero deadline: the session cap is not what this test covers.
		HubServe(conn, "test.local", time.Time{})
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cl, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cl.CloseNow()

	clients := func() int {
		h.mu.Lock()
		defer h.mu.Unlock()
		return len(h.clients)
	}

	sub := `{"op":"subscribe","topic":"status","params":` + testStatusParams + `}`
	if err := cl.Write(ctx, websocket.MessageText, []byte(sub)); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}
	_, data, err := cl.Read(ctx)
	if err != nil {
		t.Fatalf("read answer: %v", err)
	}
	var env struct {
		Topic string                 `json:"topic"`
		Data  map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("answer is not valid JSON: %v", err)
	}
	if env.Topic != "status" {
		t.Errorf("topic = %q, want status", env.Topic)
	}
	if _, ok := env.Data["cpu"]; !ok {
		t.Errorf("requested resource missing from the sample: %v", env.Data)
	}

	if got := clients(); got != 1 {
		t.Fatalf("registered clients = %d, want 1", got)
	}

	_ = cl.Close(websocket.StatusNormalClosure, "")
	deadline := time.Now().Add(5 * time.Second)
	for clients() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := clients(); got != 0 {
		t.Errorf("client still registered %v after disconnect: %d", time.Since(deadline), got)
	}
}
