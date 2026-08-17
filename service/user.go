package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/util"
	"github.com/shenaba/2s-ui/util/common"
)

type UserService struct {
}

// CredentialFingerprint digests the credentials a session for this username was
// issued under. Sessions are signed cookies with no server-side state
// (web.go's cookie.NewStore), so changing the password cannot invalidate the
// ones already handed out -- an old cookie keeps working until it expires on
// its own. Stamping this into the session and re-checking it on every request
// is what turns "change the password" into "log every other session out",
// including the websocket, whose handshake is the only place it authenticates.
//
// An empty result means no session can be vouched for -- a missing or
// unreadable user row -- and callers must treat it as a rejection rather than
// as a value to compare against.
//
// Read straight from the DB on every call, deliberately. Caching it and
// invalidating on the write paths looked cheaper but could not be made
// correct: `sui admin -reset` writes the row from a *separate process*, and
// ImportDB swaps the whole database file underneath a running panel -- neither
// can reach an in-process cache, so both would leave sessions alive that the
// credential change was supposed to retire. One indexed lookup per
// authenticated request is the price of that being right.
func CredentialFingerprint(username string) string {
	user := &model.User{}
	err := database.GetDB().Model(model.User{}).
		Where("username = ?", username).
		First(user).Error
	if err != nil {
		if !database.IsNotFound(err) {
			logger.Warning("credential fingerprint:", err)
		}
		return ""
	}

	// The username is part of the digest because ChangePass can rename the
	// account, and that should retire the old sessions just as a new password
	// does. The stored value is a bcrypt hash, so this digests a digest -- it
	// identifies the credentials without giving a session cookie anything
	// useful to an attacker who reads one.
	sum := sha256.Sum256([]byte(user.Username + "\x00" + user.Password))
	return hex.EncodeToString(sum[:8])
}

func (s *UserService) GetFirstUser() (*model.User, error) {
	db := database.GetDB()

	user := &model.User{}
	err := db.Model(model.User{}).
		First(user).
		Error
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *UserService) UpdateFirstUser(username string, password string) error {
	if username == "" {
		return common.NewError("username can not be empty")
	} else if password == "" {
		return common.NewError("password can not be empty")
	}
	hashedPass, err := util.HashPassword(password)
	if err != nil {
		return err
	}
	db := database.GetDB()
	user := &model.User{}
	err = db.Model(model.User{}).First(user).Error
	if database.IsNotFound(err) {
		user.Username = username
		user.Password = hashedPass
		return db.Model(model.User{}).Create(user).Error
	} else if err != nil {
		return err
	}
	user.Username = username
	user.Password = hashedPass
	return db.Save(user).Error
}

// ErrTwoFaRequired says the password checked out but the account carries a
// second factor and the request brought no code. It is the one outcome the
// caller can tell apart from a plain failure, and only ever after the password
// matched: answering it earlier would let anyone enumerate which accounts have
// 2FA enabled, and would do it from an unauthenticated request.
var ErrTwoFaRequired = common.NewErrorf("two-factor code required")

func (s *UserService) Login(username string, password string, code string, remoteIP string) (string, error) {
	user, err := s.CheckUser(username, password, code, remoteIP)
	if err != nil {
		return "", err
	}
	return user.Username, nil
}

// CheckUser authenticates one login attempt. Every rejection except
// ErrTwoFaRequired carries the same message -- an unknown user, a wrong
// password and a wrong TOTP code are indistinguishable to the caller, so the
// response cannot be used to work out which half was right.
func (s *UserService) CheckUser(username string, password string, code string, remoteIP string) (*model.User, error) {
	db := database.GetDB()
	rejected := common.NewError("wrong user or password! IP: ", remoteIP)

	user := &model.User{}
	err := db.Model(model.User{}).
		Where("username = ?", username).
		First(user).
		Error
	if database.IsNotFound(err) {
		return nil, rejected
	} else if err != nil {
		logger.Warning("check user err:", err, " IP: ", remoteIP)
		return nil, rejected
	}

	if !util.CheckPassword(password, user.Password) {
		return nil, rejected
	}

	if user.TwoFaSecret != "" {
		if strings.TrimSpace(code) == "" {
			return nil, ErrTwoFaRequired
		}
		counter, ok := util.ValidateTOTPAfter(user.TwoFaSecret, code, user.TwoFaCounter)
		if !ok {
			return nil, rejected
		}
		// Burn the code before the login is granted. The counter condition is
		// what makes that safe under concurrency: two requests replaying the
		// same code both pass the check above, but only one can move the
		// counter, and the loser sees no affected rows and is refused.
		res := db.Model(model.User{}).
			Where("id = ? AND two_fa_counter < ?", user.Id, counter).
			Update("two_fa_counter", counter)
		if res.Error != nil {
			logger.Warning("unable to record the used 2fa counter", res.Error)
			return nil, rejected
		}
		if res.RowsAffected == 0 {
			return nil, rejected
		}
	}

	// Lazily upgrade a legacy plaintext password now that we know the plain
	// value matches -- covers rows the 1.5.2 migration hasn't touched.
	if !util.IsHashedPassword(user.Password) {
		if hashedPass, err := util.HashPassword(password); err == nil {
			if err := db.Model(model.User{}).Where("id = ?", user.Id).Update("password", hashedPass).Error; err != nil {
				logger.Warning("unable to upgrade stored password", err)
			} else {
				user.Password = hashedPass
			}
		}
	}

	lastLoginTxt := time.Now().Format("2006-01-02 15:04:05") + " " + remoteIP
	err = db.Model(model.User{}).
		Where("username = ?", username).
		Update("last_logins", &lastLoginTxt).Error
	if err != nil {
		logger.Warning("unable to log login data", err)
	}
	return user, nil
}

func (s *UserService) GetUsers() (*[]model.User, error) {
	var users []model.User
	db := database.GetDB()
	// The secret itself never leaves the panel, only whether there is one.
	// COALESCE because AutoMigrate adds the column as NULL on existing rows,
	// and NULL <> '' is NULL rather than false.
	err := db.Model(model.User{}).
		Select("id,username,last_logins,COALESCE(two_fa_secret,'') <> '' as two_fa").
		Scan(&users).Error
	if err != nil {
		return nil, err
	}
	return &users, nil
}

func (s *UserService) ChangePass(id string, oldPass string, newUser string, newPass string) error {
	db := database.GetDB()
	user := &model.User{}
	err := db.Model(model.User{}).Where("id = ?", id).First(user).Error
	if err != nil || database.IsNotFound(err) {
		return err
	}
	if !util.CheckPassword(oldPass, user.Password) {
		return common.NewError("wrong password")
	}
	hashedPass, err := util.HashPassword(newPass)
	if err != nil {
		return err
	}
	user.Username = newUser
	user.Password = hashedPass
	// Saving retires every session issued under the old credentials, this one
	// included: they carry a fingerprint of the row as it was.
	return db.Save(user).Error
}

// EnableTwoFa stores a secret only once a code generated from it has been
// shown to work. Enrolling without that check is how users lock themselves out
// of their own panel: a mistyped secret or a phone whose clock is far off
// produces an account whose second factor can never be satisfied.
func (s *UserService) EnableTwoFa(username string, secret string, code string) error {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return common.NewError("no secret to enable")
	}
	counter, ok := util.ValidateTOTPAfter(secret, code, 0)
	if !ok {
		return common.NewError("wrong code")
	}

	db := database.GetDB()
	user := &model.User{}
	if err := db.Model(model.User{}).Where("username = ?", username).First(user).Error; err != nil {
		return err
	}
	// Replacing a live secret has to go through DisableTwoFa, which asks for the
	// password. Allowing it here would make enrolment a way around that check:
	// whoever reaches an unlocked browser could rebind the second factor to
	// their own phone without ever knowing the password.
	if user.TwoFaSecret != "" {
		return common.NewError("two-factor authentication is already enabled; disable it first")
	}
	// The enrolment code counts as used, or it could be replayed to log in
	// immediately after enabling.
	return db.Model(model.User{}).Where("id = ?", user.Id).
		Updates(map[string]interface{}{"two_fa_secret": secret, "two_fa_counter": counter}).Error
}

// ClearFirstUserTwoFa turns the second factor off without checking a password,
// and exists for the cases where the password is not what is missing: a lost
// phone with no recovery code, or a clock that moved backwards past the replay
// high-water mark so every code is refused. Only the CLI calls it, which
// already requires shell access to the machine holding the database -- anyone
// who has that can edit the row by hand anyway.
func (s *UserService) ClearFirstUserTwoFa() error {
	db := database.GetDB()
	user := &model.User{}
	if err := db.Model(model.User{}).First(user).Error; err != nil {
		return err
	}
	return db.Model(model.User{}).Where("id = ?", user.Id).
		Updates(map[string]interface{}{"two_fa_secret": "", "two_fa_counter": 0}).Error
}

// DisableTwoFa needs the account password. The session alone is not enough:
// turning the second factor off is exactly what someone who has got hold of an
// unattended browser would want to do.
func (s *UserService) DisableTwoFa(username string, password string) error {
	db := database.GetDB()
	user := &model.User{}
	if err := db.Model(model.User{}).Where("username = ?", username).First(user).Error; err != nil {
		return err
	}
	if !util.CheckPassword(password, user.Password) {
		return common.NewError("wrong password")
	}
	// The counter goes with the secret: a later re-enrolment mints a new one,
	// and leaving a high-water mark behind would refuse its early codes.
	return db.Model(model.User{}).Where("id = ?", user.Id).
		Updates(map[string]interface{}{"two_fa_secret": "", "two_fa_counter": 0}).Error
}

func (s *UserService) LoadTokens() ([]byte, error) {
	db := database.GetDB()
	var tokens []model.Tokens
	err := db.Model(model.Tokens{}).Preload("User").Where("expiry == 0 or expiry > ?", time.Now().Unix()).Find(&tokens).Error
	if err != nil {
		return nil, err
	}
	var result []map[string]interface{}
	for _, t := range tokens {
		result = append(result, map[string]interface{}{
			"token":    t.Token,
			"expiry":   t.Expiry,
			"username": t.User.Username,
		})
	}
	jsonResult, _ := json.MarshalIndent(result, "", "  ")
	return jsonResult, nil
}

func (s *UserService) GetUserTokens(username string) (*[]model.Tokens, error) {
	db := database.GetDB()
	var token []model.Tokens
	err := db.Model(model.Tokens{}).Select("id,desc,'****' as token,expiry,user_id").Where("user_id = (select id from users where username = ?)", username).Find(&token).Error
	if err != nil && !database.IsNotFound(err) {
		println(err.Error())
		return nil, err
	}
	return &token, nil
}

func (s *UserService) AddToken(username string, expiry int64, desc string) (string, error) {
	db := database.GetDB()
	var userId uint
	err := db.Model(model.User{}).Where("username = ?", username).Select("id").Scan(&userId).Error
	if err != nil {
		return "", err
	}
	if expiry > 0 {
		expiry = expiry*86400 + time.Now().Unix()
	}
	token := &model.Tokens{
		Token:  common.Random(32),
		Desc:   desc,
		Expiry: expiry,
		UserId: userId,
	}
	err = db.Create(token).Error
	if err != nil {
		return "", err
	}
	return token.Token, nil
}

func (s *UserService) DeleteToken(id string) error {
	db := database.GetDB()
	return db.Model(model.Tokens{}).Where("id = ?", id).Delete(&model.Tokens{}).Error
}
