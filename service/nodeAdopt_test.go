package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"

	"github.com/op/go-logging"
)

// A node offering one inbound of each kind that matters to client binding:
// a vless that takes users, and three that do not -- a managed shadowsocks, a
// shadowtls v2 and a tun. The list carries "users" exactly where the node's own
// takesUsers holds, as GetAll writes it. n-future is a type this panel has
// never heard of, which a newer node lists users for.
const adoptNodeList = `{"success":true,"obj":{"inbounds":[
	{"id":1,"type":"vless","tag":"n-vless","users":[]},
	{"id":2,"type":"shadowsocks","tag":"n-ss-managed"},
	{"id":3,"type":"shadowtls","tag":"n-stls2","version":2},
	{"id":4,"type":"tun","tag":"n-tun"},
	{"id":5,"type":"futureproto","tag":"n-future","users":[]}]}}`

var adoptNodeFull = map[string]string{
	"1": `{"id":1,"type":"vless","tag":"n-vless","listen_port":443,"addrs":[],"out_json":{"server":"node.example","server_port":443}}`,
	"2": `{"id":2,"type":"shadowsocks","tag":"n-ss-managed","listen_port":8388,"method":"2022-blake3-aes-128-gcm","managed":true}`,
	"3": `{"id":3,"type":"shadowtls","tag":"n-stls2","listen_port":8443,"version":2}`,
	"4": `{"id":4,"type":"tun","tag":"n-tun"}`,
	"5": `{"id":5,"type":"futureproto","tag":"n-future","listen_port":9000}`,
}

func adoptTestNode(t *testing.T) (nodeId uint, requests *atomic.Int32) {
	t.Helper()
	logger.InitLogger(logging.CRITICAL)
	if err := database.InitDB(filepath.Join(t.TempDir(), "adopt.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDBForTest() })
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}

	requests = &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		ids := r.URL.Query().Get("id")
		if ids == "" {
			_, _ = w.Write([]byte(adoptNodeList))
			return
		}
		var rows []string
		for _, id := range strings.Split(ids, ",") {
			rows = append(rows, adoptNodeFull[id])
		}
		_, _ = w.Write([]byte(`{"success":true,"obj":{"inbounds":[` + strings.Join(rows, ",") + `]}}`))
	}))
	t.Cleanup(srv.Close)

	node := model.Node{Name: "tokyo", Enable: true, BaseUrl: srv.URL}
	if err := database.GetDB().Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	return node.Id, requests
}

func TestFetchNodeInboundsReportsTakesUsers(t *testing.T) {
	nodeId, _ := adoptTestNode(t)
	list, err := (&NodeSyncService{}).FetchNodeInbounds(nodeId)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, ib := range list {
		got[ib.Tag] = ib.TakesUsers
	}
	want := map[string]bool{"n-vless": true, "n-ss-managed": false, "n-stls2": false, "n-tun": false, "n-future": false}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("takesUsers = %v, want %v", got, want)
	}
}

// Issue #287: adoption binds the named clients, through the same path a new
// local inbound uses, to the adopted replicas that take users -- and only those.
func TestAdoptInboundsBindsClients(t *testing.T) {
	nodeId, _ := adoptTestNode(t)
	db := database.GetDB()

	cfg := json.RawMessage(`{"vless": {"name": "x", "uuid": "8b5e6d3c-3f3e-4c1a-9b1e-2b8a4f6f0a11"}}`)
	alice := model.Client{Enable: true, Name: "alice", Config: cfg, Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`)}
	bob := model.Client{Enable: true, Name: "bob", Group: "team", Config: cfg, Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`)}
	// Owned by a master this panel is itself a node of.
	upstream := model.Client{Enable: true, Name: "up", Group: clusterGroup, Config: cfg, Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`)}
	for _, c := range []*model.Client{&alice, &bob, &upstream} {
		if err := db.Create(c).Error; err != nil {
			t.Fatal(err)
		}
	}

	// n-missing is not on the node: the rest still import, as before #287.
	tags := []string{"n-vless", "n-ss-managed", "n-stls2", "n-tun", "n-future", "n-missing"}
	initUsers := ids(alice.Id, bob.Id, upstream.Id, 999999)
	if err := (&NodeSyncService{}).AdoptInbounds(nodeId, tags, initUsers, "admin"); err != nil {
		t.Fatalf("adopt: %v", err)
	}

	var replicas []model.Inbound
	if err := db.Where("node_id = ?", nodeId).Find(&replicas).Error; err != nil {
		t.Fatal(err)
	}
	byTag := map[string]uint{}
	for _, r := range replicas {
		byTag[r.Tag] = r.Id
	}
	if len(byTag) != 5 {
		t.Fatalf("adopted %v, want the five the node has", byTag)
	}

	inboundsOf := func(id uint) (ids []uint, links string) {
		t.Helper()
		var c model.Client
		if err := db.First(&c, id).Error; err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(c.Inbounds, &ids); err != nil {
			t.Fatal(err)
		}
		return ids, string(c.Links)
	}
	for _, c := range []model.Client{alice, bob} {
		ids, links := inboundsOf(c.Id)
		if want := []uint{byTag["n-vless"]}; !reflect.DeepEqual(ids, want) {
			t.Errorf("%s: inbounds %v, want only the vless replica %v", c.Name, ids, want)
		}
		// The replica's link is the node's, folded in on reconcile.
		if strings.Contains(links, "vless://") {
			t.Errorf("%s: replica got a local link: %s", c.Name, links)
		}
	}
	if ids, _ := inboundsOf(upstream.Id); len(ids) != 0 {
		t.Errorf("@cluster client was bound: %v", ids)
	}

	var change model.Changes
	if err := db.Where("action = ?", "adopt").Last(&change).Error; err != nil {
		t.Fatal(err)
	}
	var obj struct {
		InitUsers []uint `json:"initUsers"`
	}
	if err := json.Unmarshal(change.Obj, &obj); err != nil {
		t.Fatal(err)
	}
	sort.Slice(obj.InitUsers, func(i, j int) bool { return obj.InitUsers[i] < obj.InitUsers[j] })
	if want := []uint{alice.Id, bob.Id}; !reflect.DeepEqual(obj.InitUsers, want) {
		t.Errorf("audit initUsers = %v, want the ids actually bound %v", obj.InitUsers, want)
	}
}

func TestAdoptInboundsRefusesBadInitUsersBeforeTheNode(t *testing.T) {
	nodeId, requests := adoptTestNode(t)
	if err := (&NodeSyncService{}).AdoptInbounds(nodeId, []string{"n-vless"}, "1,x", "admin"); err == nil {
		t.Fatal("a malformed initUsers id was accepted")
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("node was contacted %d times before the request was validated", n)
	}
	var count int64
	database.GetDB().Model(model.Inbound{}).Count(&count)
	if count != 0 {
		t.Errorf("%d replicas created by a refused request", count)
	}
}

// The master reads "takes users" off a node's list by the presence of its
// "users" key; GetAll must therefore emit it by takesUsers exactly.
func TestGetAllUsersKeyFollowsTakesUsers(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "getall.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDBForTest() })
	rows := []model.Inbound{
		{Type: "vless", Tag: "v", Options: json.RawMessage(`{"listen_port":1}`)},
		{Type: "shadowsocks", Tag: "ss", Options: json.RawMessage(`{"listen_port":2,"method":"aes-128-gcm"}`)},
		{Type: "shadowsocks", Tag: "ssm", Options: json.RawMessage(`{"listen_port":3,"method":"2022-blake3-aes-128-gcm","managed":true}`)},
		{Type: "shadowtls", Tag: "st2", Options: json.RawMessage(`{"listen_port":4,"version":2}`)},
		{Type: "shadowtls", Tag: "st3", Options: json.RawMessage(`{"listen_port":5,"version":3}`)},
		{Type: "tun", Tag: "t", Options: json.RawMessage(`{}`)},
	}
	for i := range rows {
		if err := database.GetDB().Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	list, err := (&InboundService{}).GetAll()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, ib := range *list {
		_, has := ib["users"]
		got[ib["tag"].(string)] = has
	}
	want := map[string]bool{"v": true, "ss": true, "ssm": false, "st2": false, "st3": true, "t": false}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("users key present = %v, want %v", got, want)
	}
}

// Clients were asked for and nobody can be bound: an error, and nothing is
// imported, rather than an import-only success the caller mistakes for access.
func TestAdoptInboundsRefusesABindToNobody(t *testing.T) {
	for _, tc := range []struct {
		name string
		tags []string
		who  func(alice, upstream uint) string
		says string // the error names which half came up empty
	}{
		{"no selected inbound takes users", []string{"n-ss-managed", "n-tun", "n-future"},
			func(alice, _ uint) string { return ids(alice) }, "takes users"},
		{"every named client is deleted or @cluster", []string{"n-vless"},
			func(_, upstream uint) string { return ids(upstream, 999999) }, "can be bound"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nodeId, _ := adoptTestNode(t)
			db := database.GetDB()
			alice := model.Client{Enable: true, Name: "alice", Inbounds: json.RawMessage(`[]`)}
			upstream := model.Client{Enable: true, Name: "up", Group: clusterGroup, Inbounds: json.RawMessage(`[]`)}
			for _, c := range []*model.Client{&alice, &upstream} {
				if err := db.Create(c).Error; err != nil {
					t.Fatal(err)
				}
			}
			err := (&NodeSyncService{}).AdoptInbounds(nodeId, tc.tags, tc.who(alice.Id, upstream.Id), "admin")
			if err == nil {
				t.Fatal("adopt succeeded while binding nobody")
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("error %q does not say %q", err, tc.says)
			}
			var count int64
			db.Model(model.Inbound{}).Count(&count)
			if count != 0 {
				t.Errorf("%d replicas kept by a refused adopt", count)
			}
		})
	}
	// Without clients the same inbounds import fine.
	nodeId, _ := adoptTestNode(t)
	if err := (&NodeSyncService{}).AdoptInbounds(nodeId, []string{"n-tun"}, "", "admin"); err != nil {
		t.Fatalf("import-only adopt: %v", err)
	}
}

// ids is the comma-separated form initUsers takes.
func ids(v ...uint) string {
	parts := make([]string, len(v))
	for i, id := range v {
		parts[i] = strconv.FormatUint(uint64(id), 10)
	}
	return strings.Join(parts, ",")
}
