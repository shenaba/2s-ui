package api

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/shenaba/2s-ui/logger"

	"github.com/gin-gonic/gin"
	"github.com/op/go-logging"
)

// gin answers an object it cannot encode with a 200 and an empty body. The
// reply says it could not be encoded instead.
func TestJsonObjReportsAnUnencodableReply(t *testing.T) {
	logger.InitLogger(logging.ERROR)
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name    string
		obj     interface{}
		success bool
	}{
		{"encodable", map[string]int{"a": 1}, true},
		{"unencodable", json.RawMessage(`{not json`), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			jsonObj(c, tc.obj, nil)
			var reply Msg
			if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
				t.Fatalf("body %q is not JSON: %v", w.Body.String(), err)
			}
			if reply.Success != tc.success {
				t.Fatalf("success = %v, want %v (%s)", reply.Success, tc.success, w.Body.String())
			}
			if got := w.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
				t.Fatalf("content type %q", got)
			}
		})
	}
	// An error reply is still an error reply.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	jsonMsg(c, "save", errors.New("boom"))
	var reply Msg
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || reply.Success || reply.Msg != "save: boom" {
		t.Fatalf("error reply %q (%v)", w.Body.String(), err)
	}
}
