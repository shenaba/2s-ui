package service

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
)

// TestBuildNodeHTTPClientBoundsIdleConns pins the backstop: a custom Transport
// inherits none of http.DefaultTransport's pool settings, and a zero
// IdleConnTimeout means an idle connection is never reaped. The node panel's
// own http.Server sets no IdleTimeout either, so a zero here is one permanently
// ESTABLISHED socket per client that forgets to close its pool (issue #176).
func TestBuildNodeHTTPClientBoundsIdleConns(t *testing.T) {
	client := buildNodeHTTPClient(&model.Node{BaseUrl: "https://node.example:2095", Insecure: true})
	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("https node: want a custom *http.Transport, got %T", client.Transport)
	}
	if tr.IdleConnTimeout <= 0 {
		t.Fatalf("https node: IdleConnTimeout = %v, want a positive timeout", tr.IdleConnTimeout)
	}

	// Plain http deliberately leaves Transport nil so it shares
	// http.DefaultTransport, which already bounds its own pool.
	client = buildNodeHTTPClient(&model.Node{BaseUrl: "http://node.example:2095"})
	if client.Transport != nil {
		t.Fatalf("http node: want the default transport, got %T", client.Transport)
	}
}

// closeIdle must not touch http.DefaultTransport. A plain-http node's client
// has a nil Transport, and http.Client.CloseIdleConnections falls back to the
// default transport when it does -- so calling it there empties a pool shared
// with the update check, warp, cmd/setting and notify, once a minute for as
// long as that node exists.
func TestCloseIdleSparesTheSharedTransport(t *testing.T) {
	var closed atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true,"obj":{}}`))
	}))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			closed.Add(1)
		}
	}
	srv.Start()
	defer srv.Close()

	// Stand in for any other caller in the process: a client with no Transport
	// of its own, leaving one connection in the shared pool.
	shared := &http.Client{Timeout: 5 * time.Second}
	resp, err := shared.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	closeIdle(buildNodeHTTPClient(&model.Node{BaseUrl: "http://node.example:2095"}))

	// The teardown would be asynchronous, so give it the room it would need.
	time.Sleep(200 * time.Millisecond)
	if n := closed.Load(); n > 0 {
		t.Fatalf("closeIdle tore down %d shared connection(s); it must skip a client with no Transport of its own", n)
	}

	// It still has to do its job for a client that does own its pool.
	own := buildNodeHTTPClient(&model.Node{BaseUrl: "https://node.example:2095"})
	if own.Transport == nil {
		t.Fatal("https node should own its transport")
	}
	closeIdle(own) // must not panic, and must reach the client's own pool
}

// connCounter watches an httptest server's sockets so a test can ask how many
// are still established.
type connCounter struct {
	mu   sync.Mutex
	live map[net.Conn]bool
	peak int
}

func (c *connCounter) state(conn net.Conn, state http.ConnState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch state {
	case http.StateNew:
		c.live[conn] = true
		if len(c.live) > c.peak {
			c.peak = len(c.live)
		}
	case http.StateClosed, http.StateHijacked:
		delete(c.live, conn)
	}
}

// settle waits for the server to notice connections the client has hung up on
// — ConnState fires asynchronously — and reports what is left.
func (c *connCounter) settle(want int) (open int, peak int) {
	deadline := time.Now().Add(3 * time.Second)
	for {
		c.mu.Lock()
		open, peak = len(c.live), c.peak
		c.mu.Unlock()
		if open <= want || time.Now().After(deadline) {
			return open, peak
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestNodeRequestsDoNotAccumulateConnections is the regression test for #176.
// Every short-lived node client builds its own Transport, whose idle pool
// outlives the client and is never collected, so repeated rounds used to leave
// one ESTABLISHED socket behind each — roughly 1,400 per node per day from the
// per-minute traffic collection alone. FetchNodeInbounds stands in for the
// whole family: it is the shortest real path through nodePushClient.
func TestNodeRequestsDoNotAccumulateConnections(t *testing.T) {
	counter := &connCounter{live: map[net.Conn]bool{}}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"obj":{"inbounds":[]}}`))
	}))
	srv.Config.ConnState = counter.state
	srv.StartTLS()
	defer srv.Close()

	// getNodeById reads the package-level handle, so the DB has to be
	// installed globally; see TestExpectedClientsCarriesLimitIp for why the
	// cleanup order matters on Windows.
	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "test.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	node := model.Node{
		Name: "n1", BaseUrl: srv.URL, WebPath: "/app/",
		Token: "t", Insecure: true, Enable: true,
	}
	if err := database.GetDB().Create(&node).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var s NodeSyncService
	const rounds = 15
	for i := 0; i < rounds; i++ {
		if _, err := s.FetchNodeInbounds(node.Id); err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
	}

	if open, peak := counter.settle(0); open > 0 {
		t.Fatalf("after %d rounds: %d connections still established (peak %d), want 0", rounds, open, peak)
	}
}
