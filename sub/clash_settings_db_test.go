package sub

import (
	"path/filepath"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
)

func openClashTestSettings(t *testing.T) {
	t.Helper()
	if err := database.OpenDB(filepath.Join(t.TempDir(), "settings.db")); err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
}
