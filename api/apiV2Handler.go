package api

import (
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/util/common"

	"github.com/gin-gonic/gin"
)

type TokenInMemory struct {
	Token    string
	Expiry   int64
	Username string
}

type APIv2Handler struct {
	ApiService
	// tokens is read on every apiv2 request (checkToken) and replaced by
	// ReloadTokens after a token add/delete; the master calls apiv2 constantly,
	// so guard both against a data race.
	tokensMu sync.RWMutex
	tokens   *[]TokenInMemory
}

func NewAPIv2Handler(g *gin.RouterGroup) *APIv2Handler {
	a := &APIv2Handler{}
	a.ReloadTokens()
	a.initRouter(g)
	return a
}

func (a *APIv2Handler) initRouter(g *gin.RouterGroup) {
	g.Use(func(c *gin.Context) {
		a.checkToken(c)
	})
	g.POST("/:postAction", a.postHandler)
	g.GET("/:getAction", a.getHandler)
}

func (a *APIv2Handler) postHandler(c *gin.Context) {
	// Set by checkToken. A second findUsername could disagree with the one
	// that passed auth (ReloadTokens swap or an expiry tick in between) and
	// attribute the write to an empty actor.
	username := c.GetString("username")
	action := c.Param("postAction")

	switch action {
	case "save":
		// sync=true asks for the immediate node fanout the web UI always gets.
		// It controls latency only — without it a change still reaches nodes
		// via the hourly ReconcileAllOnline safety net. Loop safety for pushed
		// changes lives in expectedClients' node_id scoping, not in this flag.
		// Malformed values are rejected: silently degrading to "no fanout"
		// would report success while nodes stay stale for up to an hour.
		fanout := false
		if v := c.Request.FormValue("sync"); v != "" {
			parsed, err := strconv.ParseBool(v)
			if err != nil {
				jsonMsg(c, "sync", err)
				return
			}
			fanout = parsed
		}
		hostname, err := a.canonicalHost(c)
		if err != nil {
			jsonMsg(c, "", err)
			return
		}
		a.ApiService.Save(c, username, fanout, hostname)
	case "restartApp":
		a.ApiService.RestartApp(c)
	case "restartSb":
		a.ApiService.RestartSb(c)
	case "resetTraffic":
		a.ApiService.ResetTraffic(c)
	case "linkConvert":
		a.ApiService.LinkConvert(c)
	case "subConvert":
		a.ApiService.SubConvert(c)
	case "testAcme":
		a.ApiService.TestAcme(c)
	case "importdb":
		a.ApiService.ImportDb(c)
	case "getCertPing":
		a.ApiService.GetCertPing(c)
	default:
		jsonMsg(c, "failed", common.NewError("unknown action: ", action))
	}
}

func (a *APIv2Handler) getHandler(c *gin.Context) {
	action := c.Param("getAction")

	switch action {
	case "load":
		a.ApiService.LoadData(c)
	case "inbounds", "outbounds", "endpoints", "services", "tls", "clients", "config":
		err := a.ApiService.LoadPartialData(c, []string{action})
		if err != nil {
			jsonMsg(c, action, err)
		}
		return
	case "users":
		a.ApiService.GetUsers(c)
	case "settings":
		a.ApiService.GetSettings(c)
	case "stats":
		a.ApiService.GetStats(c)
	case "status":
		a.ApiService.GetStatus(c)
	case "onlines":
		a.ApiService.GetOnlines(c)
	case "onlineIps":
		a.ApiService.GetOnlineIps(c)
	case "logs":
		a.ApiService.GetLogs(c)
	case "changes":
		a.ApiService.CheckChanges(c)
	case "keypairs":
		a.ApiService.GetKeypairs(c)
	case "getdb":
		a.ApiService.GetDb(c)
	case "checkOutbound":
		a.ApiService.GetCheckOutbound(c)
	default:
		jsonMsg(c, "failed", common.NewError("unknown action: ", action))
	}
}

func (a *APIv2Handler) findUsername(c *gin.Context) string {
	token := c.Request.Header.Get("Token")
	now := time.Now().Unix()
	a.tokensMu.RLock()
	defer a.tokensMu.RUnlock()
	if a.tokens == nil {
		return ""
	}
	// Read-only: expired entries are skipped, not spliced out mid-range (that
	// skipped the following token); physical removal happens in ReloadTokens.
	for _, t := range *a.tokens {
		if t.Expiry > 0 && t.Expiry < now {
			continue
		}
		if t.Token == token {
			return t.Username
		}
	}
	return ""
}

func (a *APIv2Handler) checkToken(c *gin.Context) {
	username := a.findUsername(c)
	if username != "" {
		c.Set("username", username)
		c.Next()
		return
	}
	jsonMsg(c, "", common.NewError("invalid token"))
	c.Abort()
}

func (a *APIv2Handler) ReloadTokens() {
	tokens, err := a.ApiService.LoadTokens()
	if err != nil {
		logger.Error("unable to load tokens: ", err)
		return
	}
	var newTokens []TokenInMemory
	if err = json.Unmarshal(tokens, &newTokens); err != nil {
		logger.Error("unable to load tokens: ", err)
	}
	a.tokensMu.Lock()
	a.tokens = &newTokens
	a.tokensMu.Unlock()
}
