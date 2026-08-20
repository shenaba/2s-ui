package service

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/util"
)

func TestEnableTwoFaRequiresCurrentPassword(t *testing.T) {
	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "user.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	users := UserService{}
	if err := users.UpdateFirstUser("admin", "correct-password"); err != nil {
		t.Fatalf("set password: %v", err)
	}
	secret, err := util.GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	code := totpCodeForTest(t, secret, time.Now())

	if err := users.EnableTwoFa("admin", "wrong-password", secret, code); err == nil || strings.TrimSpace(err.Error()) != "wrong password" {
		t.Fatalf("wrong password returned %v, want wrong password", err)
	}
	var afterWrong model.User
	if err := database.GetDB().First(&afterWrong).Error; err != nil {
		t.Fatalf("read user: %v", err)
	}
	if afterWrong.TwoFaSecret != "" {
		t.Fatal("wrong password stored the candidate secret")
	}

	if err := users.EnableTwoFa("admin", "correct-password", secret, code); err != nil {
		t.Fatalf("enable with current password: %v", err)
	}
	var enabled model.User
	if err := database.GetDB().First(&enabled).Error; err != nil {
		t.Fatalf("read enabled user: %v", err)
	}
	if enabled.TwoFaSecret != secret {
		t.Errorf("stored secret = %q, want candidate", enabled.TwoFaSecret)
	}
}

func totpCodeForTest(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	encoding := base32.StdEncoding.WithPadding(base32.NoPadding)
	key, err := encoding.DecodeString(secret)
	if err != nil {
		t.Fatalf("decode generated secret: %v", err)
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(at.Unix()/30))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1_000_000)
}
