package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/core"
	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service"

	"github.com/gin-gonic/gin"
	logging "github.com/op/go-logging"
)

func postMaintenance(t *testing.T, body string) Msg {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/maintenance", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	(&ApiService{}).SetMaintenance(c)

	var msg Msg
	if err := json.Unmarshal(w.Body.Bytes(), &msg); err != nil {
		t.Fatalf("decode reply for %q: %v (%s)", body, err, w.Body.String())
	}
	return msg
}

// The switch is the difference between a panel serving clients and one that is
// not, so a request that does not clearly say which way it wants it must be
// refused. Reading the value as `== "true"` would make every typo a resume.
func TestMaintenanceNeedsAnExplicitBool(t *testing.T) {
	logger.InitLogger(logging.ERROR)
	if err := database.InitDB(filepath.Join(t.TempDir(), "maintenance.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	svc := service.NewConfigService(core.NewCore())
	t.Cleanup(func() {
		_ = svc.SetMaintenance(false)
		_ = svc.StopCore()
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	if err := svc.SetMaintenance(true); err != nil {
		t.Fatalf("SetMaintenance(true): %v", err)
	}

	for _, body := range []string{"", "enable=", "enable=perhaps", "enabled=false", "enable=0.0"} {
		msg := postMaintenance(t, body)
		if msg.Success {
			t.Errorf("%q was accepted", body)
		}
		if !svc.InMaintenance() {
			t.Fatalf("%q put the panel back in service", body)
		}
	}

	// And the two it does accept say which way they went: the panel turns the
	// message into the toast, so one label for both would confirm nothing.
	if msg := postMaintenance(t, "enable=false"); !msg.Success || msg.Msg != "maintenanceOff" {
		t.Errorf("enable=false -> %+v, want success with maintenanceOff", msg)
	}
	if svc.InMaintenance() {
		t.Error("enable=false left the panel out of service")
	}
	if msg := postMaintenance(t, "enable=true"); !msg.Success || msg.Msg != "maintenanceOn" {
		t.Errorf("enable=true -> %+v, want success with maintenanceOn", msg)
	}
	if !svc.InMaintenance() {
		t.Error("enable=true left the panel in service")
	}
}
