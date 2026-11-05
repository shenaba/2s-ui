package api

import (
	"path/filepath"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/middleware"
)

func loginEngine(t *testing.T) *gin.Engine {
	t.Helper()
	if err := database.InitDB(filepath.Join(t.TempDir(), "test.db")); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if db := database.GetDB(); db != nil {
			if sqlDB, err := db.DB(); err == nil {
				sqlDB.Close()
			}
		}
	})

	engine := gin.New()
	store := cookie.NewStore([]byte("test-secret-not-used-anywhere-else"))
	store.Options(BaseSessionOptions(0))
	engine.Use(sessions.Sessions("s-ui", store))

	apiv2 := NewAPIv2Handler(engine.Group("/apiv2"))
	NewAPIHandler(engine.Group("/api", middleware.SameOrigin(middleware.Options{})), apiv2)
	return engine
}
