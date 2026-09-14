package migration

import (
	"encoding/json"
	"testing"

	"github.com/shenaba/2s-ui/database/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func newPortMigrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	// One connection, or each pooled connection gets its own empty :memory: db.
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&model.Outbound{}, &model.Inbound{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func rawColumn(t *testing.T, db *gorm.DB, query string, args ...interface{}) map[string]interface{} {
	t.Helper()
	// A string, not []byte: gorm reads a []byte destination as a slice of rows.
	var raw string
	if err := db.Raw(query, args...).Scan(&raw).Error; err != nil {
		t.Fatalf("read column: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return decoded
}

// Importing a vmess share link stored its port as the string the link format
// uses, and sing-box declares server_port a uint16 -- which fails the whole
// core config, not the one outbound, so the core will not start at all.
func TestTo1_8_2RepairsStringOutboundPort(t *testing.T) {
	db := newPortMigrationDB(t)

	tests := []struct {
		name    string
		options string
		want    interface{} // nil = the field must be left exactly as it was
	}{
		{"a string port is rewritten", `{"server_port":"443","uuid":"u"}`, float64(443)},
		{"a number is already right", `{"server_port":443,"uuid":"u"}`, float64(443)},
		{"not a port at all is left alone", `{"server_port":"not-a-port","uuid":"u"}`, "not-a-port"},
		{"out of range is left alone", `{"server_port":"70000","uuid":"u"}`, "70000"},
	}
	for i, tt := range tests {
		if err := db.Exec("INSERT INTO outbounds (id, type, tag, options) VALUES (?,?,?,?)",
			i+1, "vmess", tt.name, tt.options).Error; err != nil {
			t.Fatalf("seed outbound: %v", err)
		}
	}

	if err := to1_8_2(db); err != nil {
		t.Fatalf("to1_8_2: %v", err)
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := rawColumn(t, db, "SELECT options FROM outbounds WHERE id = ?", i+1)
			if got := options["server_port"]; got != tt.want {
				t.Errorf("server_port = %#v (%T), want %#v", got, got, tt.want)
			}
			if options["uuid"] != "u" {
				t.Errorf("the rest of the options must survive, got %v", options)
			}
		})
	}
}

// sing-box parses each server_ports entry as a range and refuses a bare port at
// start-up rather than at parse, so a row spelling a single port "443" reached
// every subscriber as a config that unmarshals and then dies on
// `bad port range: 443`.
func TestTo1_8_2NormalizesStoredPortRanges(t *testing.T) {
	db := newPortMigrationDB(t)

	tests := []struct {
		name    string
		outJson string
		want    interface{}
	}{
		{"a bare single port becomes its own range",
			`{"server_ports":["443","20000:30000"],"type":"hysteria2"}`,
			[]interface{}{"443:443", "20000:30000"}},
		{"a dash range becomes a colon range",
			`{"server_ports":["20000-30000"],"type":"hysteria2"}`,
			[]interface{}{"20000:30000"}},
		{"a number becomes a range",
			`{"server_ports":[443],"type":"hysteria2"}`,
			[]interface{}{"443:443"}},
		{"an already correct row is unchanged",
			`{"server_ports":["20000:30000"],"type":"hysteria2"}`,
			[]interface{}{"20000:30000"}},
		{"nothing usable is dropped rather than carried",
			`{"server_ports":["nonsense"],"type":"hysteria2"}`,
			nil},
	}
	for i, tt := range tests {
		if err := db.Exec("INSERT INTO inbounds (id, type, tag, out_json) VALUES (?,?,?,?)",
			i+1, "hysteria2", tt.name, tt.outJson).Error; err != nil {
			t.Fatalf("seed inbound: %v", err)
		}
	}

	if err := to1_8_2(db); err != nil {
		t.Fatalf("to1_8_2: %v", err)
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outJson := rawColumn(t, db, "SELECT out_json FROM inbounds WHERE id = ?", i+1)
			got, present := outJson["server_ports"]
			if tt.want == nil {
				if present {
					t.Errorf("server_ports = %#v, want the key dropped", got)
				}
				return
			}
			if !jsonEqual(got, tt.want) {
				t.Errorf("server_ports = %#v, want %#v", got, tt.want)
			}
			if outJson["type"] != "hysteria2" {
				t.Errorf("the rest of out_json must survive, got %v", outJson)
			}
		})
	}
}

// Re-running must not keep rewriting: migrateDb calls this on every start-up
// until the version is stamped, and a repair that is not idempotent would churn
// every row each time.
func TestTo1_8_2IsIdempotent(t *testing.T) {
	db := newPortMigrationDB(t)
	if err := db.Exec("INSERT INTO outbounds (id, type, tag, options) VALUES (1,'vmess','ob','{\"server_port\":\"443\"}')").Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Exec("INSERT INTO inbounds (id, type, tag, out_json) VALUES (1,'hysteria2','in','{\"server_ports\":[\"443\"]}')").Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	for run := 1; run <= 2; run++ {
		if err := to1_8_2(db); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
	}
	if got := rawColumn(t, db, "SELECT options FROM outbounds WHERE id = 1")["server_port"]; got != float64(443) {
		t.Errorf("server_port = %#v, want 443", got)
	}
	got := rawColumn(t, db, "SELECT out_json FROM inbounds WHERE id = 1")["server_ports"]
	if !jsonEqual(got, []interface{}{"443:443"}) {
		t.Errorf("server_ports = %#v, want [443:443]", got)
	}
}

// A null column and a malformed one must be skipped, not panicked on: a panic
// here rolls the whole transaction back and repeats on every start-up.
func TestTo1_8_2SkipsUnreadableRows(t *testing.T) {
	db := newPortMigrationDB(t)
	if err := db.Exec("INSERT INTO outbounds (id, type, tag, options) VALUES (1,'vmess','a','null'),(2,'vmess','b','not json'),(3,'vmess','c','')").Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Exec("INSERT INTO inbounds (id, type, tag, out_json) VALUES (1,'hysteria2','a','null'),(2,'hysteria2','b','not json')").Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := to1_8_2(db); err != nil {
		t.Fatalf("to1_8_2 must skip what it cannot read, got: %v", err)
	}
}

func jsonEqual(a, b interface{}) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}
