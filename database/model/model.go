package model

import "encoding/json"

type Setting struct {
	Id    uint   `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Key   string `json:"key" form:"key"`
	Value string `json:"value" form:"value"`
}

type Tls struct {
	Id     uint            `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Name   string          `json:"name" form:"name"`
	Server json.RawMessage `json:"server" form:"server"`
	Client json.RawMessage `json:"client" form:"client"`
}

type User struct {
	Id         uint   `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Username   string `json:"username" form:"username"`
	Password   string `json:"password" form:"password"`
	LastLogins string `json:"lastLogin"`
	// Base32 TOTP shared secret; empty means the second factor is off. Tagged
	// json:"-" because knowing it is enough to generate valid codes: the panel
	// hands it out exactly once, during enrolment, and never reads it back to
	// any caller afterwards.
	TwoFaSecret string `json:"-"`
	// Counter the last accepted code matched. A code stays valid for up to 90
	// seconds, so without this the same six digits could be replayed for the
	// rest of that window; anything at or below it is refused.
	TwoFaCounter int64 `json:"-"`
	// Whether TwoFaSecret is set, which is all the UI is ever told. It is
	// computed by the query that selects it, so it is read-only ("->") and
	// excluded from migrations ("-:migration") -- there is no such column.
	TwoFa bool `json:"twoFa" gorm:"->;-:migration"`
}

type LoginAttempt struct {
	Id    uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Scope string `json:"scope" gorm:"uniqueIndex:idx_login_attempt,priority:1"`
	Key   string `json:"key" gorm:"uniqueIndex:idx_login_attempt,priority:2"`
	// Failures counted since WindowStart. After a username ban is served,
	// Failures stays zero and WindowStart tracks the latest continued attack so
	// the username scope can rearm after a quiet period. Times are unix seconds.
	Failures    int   `json:"failures"`
	WindowStart int64 `json:"windowStart"`
	// Unix seconds until which logins are refused. A served username ban keeps
	// its old positive value while the row is disarmed; loginBanRemaining is the
	// authority on whether it is active. Wall-clock on purpose, like the IP
	// limit's bans: outliving the process is the whole point.
	BannedUntil int64 `json:"bannedUntil"`
}

type Client struct {
	Id       uint            `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Enable   bool            `json:"enable" form:"enable"`
	Name     string          `json:"name" form:"name"`
	Config   json.RawMessage `json:"config,omitempty" form:"config"`
	Inbounds json.RawMessage `json:"inbounds" form:"inbounds"`
	Links    json.RawMessage `json:"links,omitempty" form:"links"`
	Volume   int64           `json:"volume" form:"volume"`
	Expiry   int64           `json:"expiry" form:"expiry"`
	Down     int64           `json:"down" form:"down"`
	Up       int64           `json:"up" form:"up"`
	Desc     string          `json:"desc" form:"desc"`
	Group    string          `json:"group" form:"group"`
	Remark   string          `json:"remark" form:"remark"`

	// Timestamps (unix seconds): creation time and last time the client had traffic
	CreatedAt int64 `json:"createdAt" form:"createdAt" gorm:"default:0;not null"`
	OnlineAt  int64 `json:"onlineAt" form:"onlineAt" gorm:"default:0;not null"`

	// Cap on concurrently connected source IPs; 0 = unlimited. Enforced by the
	// panel's own periodic scan of the connection tracker, not by sing-box.
	// Indexed because the scan runs every 10s asking for the rows above zero,
	// which are the rare ones -- exactly the shape an index answers cheaply.
	LimitIp int `json:"limitIp" form:"limitIp" gorm:"default:0;not null;index"`

	// Telegram user allowed to look this client up in the bot; 0 = nobody.
	// Bound through the bot, so the panel's own client form does not carry it
	// and ClientService.preserveServerManagedFields keeps an edit from writing
	// it back to zero. Indexed because every incoming bot message resolves the
	// sender through it.
	TgId int64 `json:"tgId" form:"tgId" gorm:"default:0;not null;index"`

	// Delay start and periodic reset
	DelayStart bool  `json:"delayStart" form:"delayStart" gorm:"default:false;not null"`
	AutoReset  bool  `json:"autoReset" form:"autoReset" gorm:"default:false;not null"`
	ResetDays  int   `json:"resetDays" form:"resetDays" gorm:"default:0;not null"`
	NextReset  int64 `json:"nextReset" form:"nextReset" gorm:"default:0;not null"`
	TotalUp    int64 `json:"totalUp" form:"totalUp" gorm:"default:0;not null"`
	TotalDown  int64 `json:"totalDown" form:"totalDown" gorm:"default:0;not null"`
}

type Stats struct {
	Id        uint64 `json:"id" gorm:"primaryKey;autoIncrement"`
	DateTime  int64  `json:"dateTime" gorm:"uniqueIndex:idx_stats_bucket,priority:3"`
	Resource  string `json:"resource" gorm:"uniqueIndex:idx_stats_bucket,priority:1"`
	Tag       string `json:"tag" gorm:"uniqueIndex:idx_stats_bucket,priority:2"`
	Direction bool   `json:"direction" gorm:"uniqueIndex:idx_stats_bucket,priority:4"`
	Traffic   int64  `json:"traffic"`
}

type Changes struct {
	Id       uint64          `json:"id" gorm:"primaryKey;autoIncrement"`
	DateTime int64           `json:"dateTime"`
	Actor    string          `json:"actor"`
	Key      string          `json:"key"`
	Action   string          `json:"action"`
	Obj      json.RawMessage `json:"obj"`
}

type Tokens struct {
	Id     uint   `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Desc   string `json:"desc" form:"desc"`
	Token  string `json:"token" form:"token"`
	Expiry int64  `json:"expiry" form:"expiry"`
	UserId uint   `json:"userId" form:"userId"`
	User   *User  `json:"user" gorm:"foreignKey:UserId;references:Id"`
}

// Node is another 2s-ui panel managed by this one over its apiv2 (Token
// header). Only connection config and the last observed transition live in
// the DB — live status is kept in an in-memory snapshot (service/node.go).
type Node struct {
	Id       uint   `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Enable   bool   `json:"enable" form:"enable" gorm:"default:true;not null"`
	Name     string `json:"name" form:"name" gorm:"unique"`
	BaseUrl  string `json:"baseUrl" form:"baseUrl"` // scheme://host[:port], no path
	WebPath  string `json:"webPath" form:"webPath"` // remote panel web path, default "/app/"
	Token    string `json:"token,omitempty" form:"token"`
	Insecure bool   `json:"insecure" form:"insecure" gorm:"default:false;not null"`
	CertPin  string `json:"certPin" form:"certPin"` // leaf cert SHA-256, overrides Insecure
	Desc     string `json:"desc" form:"desc"`
	// Unix seconds of the last time the node was seen online; written only on
	// online -> offline/core-stopped transitions, not every heartbeat.
	LastSeen int64 `json:"lastSeen" form:"lastSeen" gorm:"default:0;not null"`

	// Dirty marks pending master-side edits that have not converged onto the
	// node yet; the heartbeat retriggers Reconcile while it is set.
	Dirty    bool  `json:"dirty" gorm:"default:false;not null"`
	LastSync int64 `json:"lastSync" gorm:"default:0;not null"`
	// Baselines holds the per-client traffic counters seen on the node at the
	// last collection: map[clientName]{up,down}. Single writer (traffic job),
	// one row UPDATE per node per cycle.
	Baselines json.RawMessage `json:"-"`
}
