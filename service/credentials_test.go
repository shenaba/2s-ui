package service

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/util"
)

// newCredentialsDB gives the test its own database with one admin account.
func newCredentialsDB(t *testing.T) UserService {
	t.Helper()
	if err := database.InitDB(filepath.Join(t.TempDir(), "user.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	users := UserService{}
	if err := users.UpdateFirstUser("admin", "correct-password"); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	return users
}

// An empty new password used to be hashed and stored, and nothing on the login
// path rejects an empty password either -- so the account was left open to
// anyone who submitted one. Only the form was stopping it.
func TestChangePassRefusesEmptyCredentials(t *testing.T) {
	users := newCredentialsDB(t)

	tests := []struct {
		name      string
		user      string
		pass      string
		wantErr   string
		loginUser string
	}{
		{"empty password", "admin", "", "password can not be empty", "admin"},
		{"empty username", "", "new-password", "username can not be empty", "admin"},
		{"no session", "admin", "new-password", "not logged in", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := users.ChangePass(tt.loginUser, "correct-password", tt.user, tt.pass)
			if err == nil {
				t.Fatalf("accepted %s", tt.name)
			}
			if got := strings.TrimSpace(err.Error()); got != tt.wantErr {
				t.Errorf("error = %q, want %q", got, tt.wantErr)
			}
		})
	}

	// The row must be untouched: the original password still works and the
	// empty one does not.
	var row model.User
	if err := database.GetDB().Model(model.User{}).First(&row).Error; err != nil {
		t.Fatalf("read user: %v", err)
	}
	if !util.CheckPassword("correct-password", row.Password) {
		t.Error("a refused change must leave the stored password alone")
	}
	if util.CheckPassword("", row.Password) {
		t.Error("an empty password must never authenticate")
	}
}

// The account to rewrite comes from the session, so a caller cannot name
// another one. The panel has a single admin today, which is why this was only
// latent -- the id simply found no row.
func TestChangePassUsesTheSessionAccount(t *testing.T) {
	users := newCredentialsDB(t)

	if err := users.ChangePass("nobody", "correct-password", "admin", "new-password"); err == nil {
		t.Error("an unknown session account must not rewrite anyone's credentials")
	}
	if err := users.ChangePass("admin", "correct-password", "admin", "new-password"); err != nil {
		t.Fatalf("the logged-in account must be able to change its own password: %v", err)
	}

	var row model.User
	if err := database.GetDB().Model(model.User{}).First(&row).Error; err != nil {
		t.Fatalf("read user: %v", err)
	}
	if !util.CheckPassword("new-password", row.Password) {
		t.Error("the new password did not land")
	}
}

// The token id came from the form with no constraint, so any authenticated
// caller could revoke another account's tokens by counting upwards.
func TestDeleteTokenChecksOwnership(t *testing.T) {
	users := newCredentialsDB(t)

	if _, err := users.AddToken("admin", 0, "mine"); err != nil {
		t.Fatalf("add token: %v", err)
	}
	var token model.Tokens
	if err := database.GetDB().Model(model.Tokens{}).First(&token).Error; err != nil {
		t.Fatalf("read token: %v", err)
	}
	id := strconv.FormatUint(uint64(token.Id), 10)

	if err := users.DeleteToken("", id); err == nil {
		t.Error("an unauthenticated caller must not delete a token")
	}
	if err := users.DeleteToken("someone-else", id); err == nil {
		t.Error("another account must not delete this token")
	}

	var count int64
	database.GetDB().Model(model.Tokens{}).Count(&count)
	if count != 1 {
		t.Fatalf("the token must still exist, got %d rows", count)
	}

	if err := users.DeleteToken("admin", id); err != nil {
		t.Fatalf("the owner must be able to delete it: %v", err)
	}
	database.GetDB().Model(model.Tokens{}).Count(&count)
	if count != 0 {
		t.Errorf("the token was not deleted, %d rows left", count)
	}

	// Deleting it twice reports rather than succeeding on zero rows, so a
	// click that did nothing does not look like it worked.
	if err := users.DeleteToken("admin", id); err == nil {
		t.Error("deleting a token that is already gone must report it")
	}
}

