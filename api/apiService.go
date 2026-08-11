package api

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shenaba/2s-ui/config"
	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service"
	"github.com/shenaba/2s-ui/util"
	"github.com/shenaba/2s-ui/util/common"

	"github.com/gin-gonic/gin"
)

type ApiService struct {
	service.SettingService
	service.UserService
	service.ConfigService
	service.ClientService
	service.TlsService
	service.InboundService
	service.OutboundService
	service.EndpointService
	service.ServicesService
	service.PanelService
	service.StatsService
	service.ServerService
	service.UpdateService
	service.NodeService
	service.NodeSyncService
}

func (a *ApiService) UpdateInfo(c *gin.Context) {
	canUpdate, reason := a.UpdateService.CanSelfUpdate()
	jsonObj(c, map[string]interface{}{
		"canSelfUpdate": canUpdate,
		"reason":        reason,
		"current":       config.GetVersion(),
		// Docker updates live in the container's writable layer; the UI warns
		// that recreating the container reverts to the image's version.
		"docker": a.UpdateService.InDocker(),
	}, nil)
}

func (a *ApiService) UpdatePanel(c *gin.Context) {
	err := a.UpdateService.StartUpdate()
	jsonMsg(c, "updatePanel", err)
}

func (a *ApiService) UpdateStatus(c *gin.Context) {
	jsonObj(c, a.UpdateService.GetStatus(), nil)
}

func (a *ApiService) LoadData(c *gin.Context) {
	data, err := a.getData(c)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonObj(c, data, nil)
}

func (a *ApiService) getData(c *gin.Context) (interface{}, error) {
	// Payload assembly lives in service.PanelDataService so the websocket hub
	// pushes exactly what this endpoint returns.
	isUpdated, err := a.ConfigService.CheckChanges(c.Query("lu"))
	if err != nil {
		return "", err
	}
	var pd service.PanelDataService
	if isUpdated {
		// Read-only path: the host is rendered, not persisted, so a settings
		// read failure degrades to the request Host rather than blanking the
		// whole panel. The save path, where it would be written, still fails.
		host, err := a.canonicalHost(c)
		if err != nil {
			logger.Warning("load: web domain unreadable, using request host: ", err)
			host = getHostname(c)
		}
		return pd.FullPayload(host)
	}
	return pd.LivePayload()
}

// canonicalHost is the hostname baked into generated links and the advertised
// subscription URI — values that outlive the request that produced them, so the
// rule for picking it lives in one place. The configured web domain wins: it is
// the spelling the operator chose, and it does not vary with how a caller
// happened to reach the panel. A settings read failure is reported rather than
// downgraded to the request Host, which is the value we must not persist.
func (a *ApiService) canonicalHost(c *gin.Context) (string, error) {
	webDomain, err := a.SettingService.GetWebDomain()
	if err != nil {
		return "", err
	}
	if host := normalizeHost(webDomain); host != "" {
		return host, nil
	}
	return getHostname(c), nil
}

func (a *ApiService) LoadPartialData(c *gin.Context, objs []string) error {
	data := make(map[string]interface{}, 0)
	id := c.Query("id")

	for _, obj := range objs {
		switch obj {
		case "inbounds":
			inbounds, err := a.InboundService.Get(id)
			if err != nil {
				return err
			}
			data[obj] = inbounds
		case "outbounds":
			outbounds, err := a.OutboundService.GetAll()
			if err != nil {
				return err
			}
			data[obj] = outbounds
		case "endpoints":
			endpoints, err := a.EndpointService.GetAll()
			if err != nil {
				return err
			}
			data[obj] = endpoints
		case "services":
			services, err := a.ServicesService.GetAll()
			if err != nil {
				return err
			}
			data[obj] = services
		case "tls":
			tlsConfigs, err := a.TlsService.GetAll()
			if err != nil {
				return err
			}
			data[obj] = tlsConfigs
		case "clients":
			// full=1 is the cluster reconcile read: it needs config to diff
			// against the master's copy. Everything else — the SPA list, the
			// websocket payload, save responses — gets the lean projection.
			var clients interface{}
			var err error
			if id == "" && c.Query("full") == "1" {
				clients, err = a.ClientService.GetAllWithConfig()
			} else {
				clients, err = a.ClientService.Get(id)
			}
			if err != nil {
				return err
			}
			data[obj] = clients
		case "config":
			config, err := a.SettingService.GetConfig()
			if err != nil {
				return err
			}
			data[obj] = json.RawMessage(config)
		case "settings":
			settings, err := a.SettingService.GetAllSetting()
			if err != nil {
				return err
			}
			data[obj] = settings
		case "nodes":
			nodes, err := a.NodeService.GetAll()
			if err != nil {
				return err
			}
			data[obj] = nodes
		}
	}

	jsonObj(c, data, nil)
	return nil
}

func (a *ApiService) GetUsers(c *gin.Context) {
	users, err := a.UserService.GetUsers()
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonObj(c, *users, nil)
}

func (a *ApiService) GetSettings(c *gin.Context) {
	data, err := a.SettingService.GetAllSetting()
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonObj(c, data, err)
}

func (a *ApiService) GetStats(c *gin.Context) {
	resource := c.Query("resource")
	tag := c.Query("tag")
	period := c.Query("period")
	if period == "" {
		period = "hour"
	}
	data, err := a.StatsService.GetStats(resource, tag, period)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonObj(c, data, err)
}

func (a *ApiService) GetStatus(c *gin.Context) {
	request := c.Query("r")
	result := a.ServerService.GetStatus(request)
	jsonObj(c, result, nil)
}

func (a *ApiService) GetOnlines(c *gin.Context) {
	onlines, err := a.StatsService.GetOnlines()
	jsonObj(c, onlines, err)
}

// GetOnlineIps lists one client's live source IPs. Kept apart from GetOnlines
// rather than added to it as a query parameter: that endpoint returns three tag
// lists, and switching its shape on a parameter would break every caller that
// does not pass one.
func (a *ApiService) GetOnlineIps(c *gin.Context) {
	jsonObj(c, gin.H{"ips": service.OnlineIPsOf(c.Query("name"))}, nil)
}

func (a *ApiService) GetLogs(c *gin.Context) {
	count := c.Query("c")
	level := c.Query("l")
	logs := a.ServerService.GetLogs(count, level)
	jsonObj(c, logs, nil)
}

func (a *ApiService) CheckChanges(c *gin.Context) {
	actor := c.Query("a")
	chngKey := c.Query("k")
	count := c.Query("c")
	changes := a.ConfigService.GetChanges(actor, chngKey, count)
	jsonObj(c, changes, nil)
}

func (a *ApiService) GetKeypairs(c *gin.Context) {
	kType := c.Query("k")
	options := c.Query("o")
	keypair := a.ServerService.GenKeypair(kType, options)
	jsonObj(c, keypair, nil)
}

func (a *ApiService) GetDb(c *gin.Context) {
	exclude := c.Query("exclude")
	db, err := database.GetDb(exclude)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", "attachment; filename=s-ui_"+time.Now().Format("20060102-150405")+".db")
	c.Writer.Write(db)
}

func (a *ApiService) postActions(c *gin.Context) (string, json.RawMessage, error) {
	var data map[string]json.RawMessage
	err := c.ShouldBind(&data)
	if err != nil {
		return "", nil, err
	}
	return string(data["action"]), data["data"], nil
}

func (a *ApiService) Login(c *gin.Context) {
	remoteIP := getRemoteIp(c)
	loginUser, err := a.UserService.Login(c.Request.FormValue("user"), c.Request.FormValue("pass"), remoteIP)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}

	sessionMaxAge, err := a.SettingService.GetSessionMaxAge()
	if err != nil {
		logger.Infof("Unable to get session's max age from DB")
	}

	err = SetLoginUser(c, loginUser, sessionMaxAge)
	if err == nil {
		logger.Info("user ", loginUser, " login success")
	} else {
		logger.Warning("login failed: ", err)
	}

	jsonMsg(c, "", nil)
}

func (a *ApiService) ChangePass(c *gin.Context) {
	id := c.Request.FormValue("id")
	oldPass := c.Request.FormValue("oldPass")
	newUsername := c.Request.FormValue("newUsername")
	newPass := c.Request.FormValue("newPass")
	err := a.UserService.ChangePass(id, oldPass, newUsername, newPass)
	if err == nil {
		logger.Info("change user credentials success")
		// Websocket auth is checked at the handshake only, so sockets opened
		// under the old credentials would otherwise keep receiving the full
		// config. Dropping them forces a fresh handshake.
		service.DropAllClients()
		jsonMsg(c, "save", nil)
	} else {
		logger.Warning("change user credentials failed:", err)
		jsonMsg(c, "", err)
	}
}

// Save handles POST api/save. fanout triggers an immediate node reconcile for
// client/inbound changes (v1 always; v2 with sync=true). It affects latency
// only — the hourly ReconcileAllOnline pushes state regardless — and is not
// what prevents mutual-master ping-pong: a pushed client references the
// receiving panel's own inbounds, so expectedClients' node_id scoping keeps it
// out of every outgoing push. hostname feeds generated links; callers pick the
// canonical value (v2 prefers the configured web domain over the request Host).
func (a *ApiService) Save(c *gin.Context, loginUser string, fanout bool, hostname string) {
	obj := c.Request.FormValue("object")
	act := c.Request.FormValue("action")
	data := c.Request.FormValue("data")
	initUsers := c.Request.FormValue("initUsers")
	objs, err := a.ConfigService.Save(obj, act, json.RawMessage(data), initUsers, loginUser, hostname)
	if err != nil {
		jsonMsg(c, "save", err)
		return
	}
	if fanout && (obj == "clients" || obj == "inbounds") {
		// Fire-and-forget: network IO must not block the save's DB txn (already
		// committed) or the HTTP response. Unrelated nodes are a cheap no-op diff.
		a.NodeSyncService.MarkAllDirty()
		go a.NodeSyncService.ReconcileDirtyOnline()
	}
	err = a.LoadPartialData(c, objs)
	if err != nil {
		jsonMsg(c, obj, err)
	}
}

func (a *ApiService) RestartApp(c *gin.Context) {
	// 单位是 time.Duration(纳秒)。这里曾传裸 3,即 3 纳秒 ≈ 立即重启,响应能否在
	// gin server 被拆掉前刷出去纯属竞态:前端 restartApp 靠 msg.success 才跳转,
	// 于是"点了没反应还弹红字"。500ms 足够刷完响应,又不引入前端要配合的长窗口。
	err := a.PanelService.RestartPanel(500 * time.Millisecond)
	jsonMsg(c, "restartApp", err)
}

func (a *ApiService) RestartSb(c *gin.Context) {
	err := a.ConfigService.RestartCore()
	jsonMsg(c, "restartSb", err)
}

func (a *ApiService) ResetTraffic(c *gin.Context) {
	if err := a.ClientService.ResetAllClientsTraffic(); err != nil {
		jsonMsg(c, "resetTraffic", err)
		return
	}
	// The reset re-enables depleted clients. DepleteJob fanned the disable out
	// to nodes, so fan the re-enable out too — otherwise nodes keep rejecting
	// paid-up users until the hourly safety net.
	a.NodeSyncService.MarkAllDirty()
	go a.NodeSyncService.ReconcileDirtyOnline()
	err := a.ConfigService.RestartCore()
	jsonMsg(c, "resetTraffic", err)
}

func (a *ApiService) LinkConvert(c *gin.Context) {
	link := c.Request.FormValue("link")
	result, _, err := util.GetOutbound(link, 0)
	jsonObj(c, result, err)
}

func (a *ApiService) SubConvert(c *gin.Context) {
	link := c.Request.FormValue("link")
	result, err := util.GetExternalSub(link)
	jsonObj(c, result, err)
}

func (a *ApiService) ImportDb(c *gin.Context) {
	file, _, err := c.Request.FormFile("db")
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	defer file.Close()
	err = database.ImportDB(file)
	jsonMsg(c, "", err)
}

func (a *ApiService) Logout(c *gin.Context) {
	loginUser := GetLoginUser(c)
	if loginUser != "" {
		logger.Infof("user %s logout", loginUser)
	}
	ClearSession(c)
	jsonMsg(c, "", nil)
}

func (a *ApiService) LoadTokens() ([]byte, error) {
	return a.UserService.LoadTokens()
}

func (a *ApiService) GetTokens(c *gin.Context) {
	loginUser := GetLoginUser(c)
	tokens, err := a.UserService.GetUserTokens(loginUser)
	jsonObj(c, tokens, err)
}

func (a *ApiService) AddToken(c *gin.Context) {
	loginUser := GetLoginUser(c)
	expiry := c.Request.FormValue("expiry")
	expiryInt, err := strconv.ParseInt(expiry, 10, 64)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	desc := c.Request.FormValue("desc")
	token, err := a.UserService.AddToken(loginUser, expiryInt, desc)
	jsonObj(c, token, err)
}

func (a *ApiService) DeleteToken(c *gin.Context) {
	tokenId := c.Request.FormValue("id")
	err := a.UserService.DeleteToken(tokenId)
	jsonMsg(c, "", err)
}

func (a *ApiService) GetSingboxConfig(c *gin.Context) {
	rawConfig, err := a.ConfigService.GetConfig("")
	if err != nil {
		c.Status(400)
		c.Writer.WriteString(err.Error())
		return
	}
	c.Header("Content-Type", "application/json")
	c.Header("Content-Disposition", "attachment; filename=config_"+time.Now().Format("20060102-150405")+".json")
	c.Writer.Write(*rawConfig)
}

func (a *ApiService) TestAcme(c *gin.Context) {
	domain := c.Request.FormValue("domain")
	email := c.Request.FormValue("email")
	if err := a.ConfigService.TestAcme(domain, email); err != nil {
		pureJsonMsg(c, false, err.Error())
		return
	}
	pureJsonMsg(c, true, "")
}

// GetCerts 列出「域名与证书」页面上的全部证书:acme.sh 托管的 + 手动登记的。
func (a *ApiService) GetCerts(c *gin.Context) {
	var certSvc service.CertService
	certs, err := certSvc.List()
	jsonObj(c, certs, err)
}

// SaveManualCert 登记或改写一份自带的证书(Cloudflare 源证书、公司内部 CA 签发的
// 那类)。acme.sh 已经在管这个域名时拒绝:两份记录指向同一域名,页面上说不清到底
// 哪份在生效,而且用户多半是想改路径、其实该去续期。
func (a *ApiService) SaveManualCert(c *gin.Context) {
	domain := strings.TrimSpace(c.Request.FormValue("domain"))
	certFile := strings.TrimSpace(c.Request.FormValue("certFile"))
	keyFile := strings.TrimSpace(c.Request.FormValue("keyFile"))

	var certSvc service.CertService
	// 查不动清单时必须失败而不是当「不存在」放行:否则一次 DB 故障就能绕过下面的
	// acme.sh 守卫,登记出一条被 List 永远隐藏的重复记录
	existing, found, err := certSvc.FindByDomain(domain)
	if err != nil {
		pureJsonMsg(c, false, err.Error())
		return
	}
	if found && existing.Managed {
		pureJsonMsg(c, false, fmt.Sprintf("%s 的证书已由 acme.sh 自动续期,无需手动登记", domain))
		return
	}
	if err := certSvc.SaveManual(domain, certFile, keyFile); err != nil {
		pureJsonMsg(c, false, err.Error())
		return
	}
	pureJsonMsg(c, true, "")
}

// DeleteCert 删除一张证书。面板或订阅正在用它时拒绝——那两个服务重启时会去读这两个
// 文件,删掉就起不来了,而此时用户多半已经离开这个页面,很难把两件事联系起来。
//
// 手动登记的只从库里移除登记,绝不碰文件本身:那是用户自己的证书,多半还被别的服务
// 用着。只有 acme.sh 托管的才走 RemoveCert(停止续期 + 删它自己产出的副本)。
func (a *ApiService) DeleteCert(c *gin.Context) {
	domain := strings.ToLower(strings.TrimSpace(c.Request.FormValue("domain")))
	if domain == "" {
		pureJsonMsg(c, false, "domain is required")
		return
	}

	var certSvc service.CertService
	// 先看它在不在 acme.sh 手里:两边都有登记时(升级归档后又申请了证书)以 acme.sh
	// 为准,否则只删掉库里那条,acme.sh 还在后台给它续期,页面上却已经看不见了。
	// 查不动清单时必须失败而不是当「不存在」:否则 acme.sh 托管的会被当成手动记录,
	// DeleteManual 删了个寂寞还报成功,acme.sh 照旧续期、那行刷新后又回来了。
	info, found, err := certSvc.FindByDomain(domain)
	if err != nil {
		pureJsonMsg(c, false, err.Error())
		return
	}

	// 占用检查两条都要:域名匹配是常规情形;路径匹配兜住「域名已经改走(或清空)、
	// 证书路径还留在设置里」的分岔——真正让服务下次重启起不来的是路径指向的文件被删,
	// 不是域名对不对得上。RemoveCert 会把 /root/cert/<域名>/ 整个目录删掉,所以设置
	// 里的路径落在该目录下也算占用。
	webDomain, _ := a.SettingService.GetWebDomain()
	subDomain, _ := a.SettingService.GetSubDomain()
	webCert, _ := a.SettingService.GetCertFile()
	webKey, _ := a.SettingService.GetKeyFile()
	subCert, _ := a.SettingService.GetSubCertFile()
	subKey, _ := a.SettingService.GetSubKeyFile()
	// Clean 之后再比:设置里的路径可能与登记的写法只差分隔符或多余的 ./
	managedDir := filepath.Clean(service.ManagedCertDir(domain)) + string(filepath.Separator)
	usesFiles := func(paths ...string) bool {
		for _, p := range paths {
			if p == "" {
				continue
			}
			p = filepath.Clean(p)
			if found && (p == filepath.Clean(info.CertFile) || p == filepath.Clean(info.KeyFile)) {
				return true
			}
			if strings.HasPrefix(p, managedDir) {
				return true
			}
		}
		return false
	}
	var inUse []string
	if strings.EqualFold(webDomain, domain) || usesFiles(webCert, webKey) {
		inUse = append(inUse, "面板")
	}
	if strings.EqualFold(subDomain, domain) || usesFiles(subCert, subKey) {
		inUse = append(inUse, "订阅")
	}
	if len(inUse) > 0 {
		pureJsonMsg(c, false, fmt.Sprintf("%s正在使用 %s 的证书;请先在对应设置里换成别的域名,再删除这张证书",
			strings.Join(inUse, "和"), domain))
		return
	}

	if found && info.Managed {
		var acme service.AcmeService
		if err := acme.RemoveCert(domain); err != nil {
			pureJsonMsg(c, false, err.Error())
			return
		}
		// 归档时留下的那条同名手动记录一并清掉,免得删完 acme.sh 那份后它又冒出来,
		// 指着一个刚被删掉的文件。对不存在的行本就是 no-op,不必先查。
		if err := certSvc.DeleteManual(domain); err != nil {
			logger.Warning("已删除 acme.sh 证书 ", domain, ",但清理登记记录失败: ", err)
		}
		pureJsonMsg(c, true, "")
		return
	}

	if err := certSvc.DeleteManual(domain); err != nil {
		pureJsonMsg(c, false, err.Error())
		return
	}
	pureJsonMsg(c, true, "")
}

func (a *ApiService) DetectNginx(c *gin.Context) {
	var acme service.AcmeService
	jsonObj(c, acme.DetectNginx(), nil)
}

// proxyForm 是前端传来的一侧(面板或订阅)的反代输入。
// 每个字段都可能缺席,缺席就回退读库——但开关、端口、路径常常是同一次改动里一起改的,
// 所以【表单值优先】:读库会拿到还没保存的旧值,生成出一份指向错误端口的配置。
type proxyForm struct {
	enabled bool
	domain  string
	path    string
	listen  string
	port    int
	cert    string
	key     string
}

func (a *ApiService) readProxyForm(c *gin.Context, prefix string, scope string) proxyForm {
	get := func(name string) string { return c.Request.FormValue(prefix + name) }

	f := proxyForm{enabled: get("Nginx") == "true"}

	// 域名的空值是有含义的(开关开着却没填域名,BuildVhostSpecs 会当场报错),不能当
	// 「没传」回退读库:那会用旧域名生成 vhost、再把空值存进库,下次启动对账读到空就跳过,
	// DB 与 nginx 从此长期不一致且不再自愈。与 ListenSet / CertSet 同理用独立标志。
	if get("DomainSet") == "true" {
		f.domain = get("Domain")
	} else if f.domain = get("Domain"); f.domain == "" {
		if scope == "web" {
			f.domain, _ = a.SettingService.GetWebDomain()
		} else {
			f.domain, _ = a.SettingService.GetSubDomain()
		}
	}
	if v := get("Port"); v != "" {
		f.port, _ = strconv.Atoi(v)
	}
	if f.port <= 0 {
		if scope == "web" {
			f.port, _ = a.SettingService.GetPort()
		} else {
			f.port, _ = a.SettingService.GetSubPort()
		}
	}
	if f.path = get("Path"); f.path == "" {
		if scope == "web" {
			f.path, _ = a.SettingService.GetWebPath()
		} else {
			f.path, _ = a.SettingService.GetSubPath()
		}
	}
	// 监听地址允许为空(= 0.0.0.0),没法用空值区分「没传」和「传了空」,
	// 所以用一个独立字段表示表单确实带了这一项。
	if get("ListenSet") == "true" {
		f.listen = get("Listen")
	} else if scope == "web" {
		f.listen, _ = a.SettingService.GetListen()
	} else {
		f.listen, _ = a.SettingService.GetSubListen()
	}
	// 证书路径的空值是有含义的(= 这个域名没有证书,vhost 生成该失败、让用户先去申请),
	// 不能当「没传」回退读库:回退读到的是上一个域名的证书,会给新域名生成一份用旧证书
	// 的 vhost——nginx -t 照样通过,保存照样继续,面板重启成明文后浏览器却因证书名不
	// 匹配进不去。与 ListenSet 同理,用独立标志表示表单确实带了这两项。
	if get("CertSet") == "true" {
		f.cert = get("CertFile")
		f.key = get("KeyFile")
	} else {
		if f.cert = get("CertFile"); f.cert == "" {
			if scope == "web" {
				f.cert, _ = a.SettingService.GetCertFile()
			} else {
				f.cert, _ = a.SettingService.GetSubCertFile()
			}
		}
		if f.key = get("KeyFile"); f.key == "" {
			if scope == "web" {
				f.key, _ = a.SettingService.GetKeyFile()
			} else {
				f.key, _ = a.SettingService.GetSubKeyFile()
			}
		}
	}
	// 大小写归一化不在这里做:BuildVhostSpecs 统一处理,它同时服务表单和启动对账
	// 两条路径,只能有一个来源。
	return f
}

// proxySides 把两侧表单值转成 BuildVhostSpecs 的输入。SyncNginxProxy 与
// CheckNginxProxy 读同一份表单、问同一批问题,只是一个落盘一个不落盘。
func (a *ApiService) proxySides(c *gin.Context) []service.ProxySide {
	web := a.readProxyForm(c, "web", "web")
	sub := a.readProxyForm(c, "sub", "sub")
	return []service.ProxySide{
		{Name: "panel", Enabled: web.enabled, Domain: web.domain, Path: web.path,
			Listen: web.listen, Port: web.port, CertFile: web.cert, KeyFile: web.key},
		{Name: "subscription", Enabled: sub.enabled, Domain: sub.domain, Path: sub.path,
			Listen: sub.listen, Port: sub.port, CertFile: sub.cert, KeyFile: sub.key},
	}
}

// SyncNginxProxy syncs the auto-generated reverse-proxy configs with the current
// form values. The frontend calls it before saving when the proxy is switched ON or
// adjusted: only once this succeeds is it safe to move the panel/subscription to
// plaintext HTTP. Switching the panel side OFF never comes through here — see
// app.syncNginxProxy for why that has to wait until after the restart.
func (a *ApiService) SyncNginxProxy(c *gin.Context) {
	var acme service.AcmeService
	// Same aggregation as the startup reconciliation; both must agree exactly
	specs, err := service.BuildVhostSpecs(a.proxySides(c)...)
	if err != nil {
		pureJsonMsg(c, false, err.Error())
		return
	}
	res, err := acme.SyncVhosts(specs)
	if err != nil {
		pureJsonMsg(c, false, err.Error())
		return
	}
	jsonMsgObj(c, "", res, nil)
}

// CheckNginxProxy is SyncNginxProxy's read-only twin: same form, same questions,
// but it writes nothing and never reloads nginx. The frontend calls it in the two
// places where writing is not an option — before saving while the proxy is already
// on, and when the settings page loads, to report drift. See service.CheckVhosts.
func (a *ApiService) CheckNginxProxy(c *gin.Context) {
	var acme service.AcmeService
	specs, err := service.BuildVhostSpecs(a.proxySides(c)...)
	if err != nil {
		pureJsonMsg(c, false, err.Error())
		return
	}
	drift, err := acme.CheckVhosts(specs)
	if err != nil {
		pureJsonMsg(c, false, err.Error())
		return
	}
	jsonMsgObj(c, "", map[string]bool{"drift": drift}, nil)
}

func (a *ApiService) IssueCert(c *gin.Context) {
	domain := c.Request.FormValue("domain")
	email := c.Request.FormValue("email")
	method := c.Request.FormValue("method")
	force := c.Request.FormValue("force") == "true"
	// 反向代理(nginx)是证书消费方时,续期后需要它 reload——由这里读设置传入,
	// AcmeService 自身不读库(见其结构体注释)。
	//
	// 判据是【这个域名有没有服务在反代后面用它】,而不是「谁发起的申请」:证书是按域名
	// 存在的,面板和订阅可能共用一张,从证书页申请时也压根没有 scope 可言。
	// 表单显式传了就以表单为准——开关改了还没保存时库里是旧值,让前端说了算。
	behindProxy := false
	if v := c.Request.FormValue("behindProxy"); v != "" {
		behindProxy = v == "true"
	} else {
		webDomain, _ := a.SettingService.GetWebDomain()
		subDomain, _ := a.SettingService.GetSubDomain()
		webNginx, _ := a.SettingService.GetWebNginx()
		subNginx, _ := a.SettingService.GetSubNginx()
		// EqualFold:域名大小写不敏感,存量设置里可能存着大小写混杂的值
		behindProxy = (webNginx && strings.EqualFold(domain, webDomain)) ||
			(subNginx && strings.EqualFold(domain, subDomain))
	}
	var acme service.AcmeService
	res, err := acme.IssueWeb(domain, email, method, force, behindProxy)
	if err != nil {
		pureJsonMsg(c, false, err.Error())
		return
	}
	jsonMsgObj(c, "", res, nil)
}

func (a *ApiService) GetCheckOutbound(c *gin.Context) {
	tag := c.Query("tag")
	link := c.Query("link")
	result := a.ConfigService.CheckOutbound(tag, link)
	jsonObj(c, result, nil)
}

func (a *ApiService) GetCertPing(c *gin.Context) {
	domain := c.PostForm("domain")
	port := c.PostForm("port")
	tlsPing, err := util.GetTlsPing(domain, port)
	jsonObj(c, tlsPing, err)
}

func (a *ApiService) TestNode(c *gin.Context) {
	data := c.PostForm("data")
	status, err := a.NodeService.TestNode(json.RawMessage(data))
	jsonObj(c, status, err)
}

func (a *ApiService) GetNodeInbounds(c *gin.Context) {
	id, err := strconv.ParseUint(c.Query("id"), 10, 64)
	if err != nil {
		jsonMsg(c, "nodeInbounds", common.NewError("invalid node id"))
		return
	}
	inbounds, err := a.NodeSyncService.FetchNodeInbounds(uint(id))
	jsonObj(c, inbounds, err)
}

func (a *ApiService) AdoptInbounds(c *gin.Context, loginUser string) {
	id, err := strconv.ParseUint(c.PostForm("id"), 10, 64)
	if err != nil {
		jsonMsg(c, "adoptInbounds", common.NewError("invalid node id"))
		return
	}
	var tags []string
	if err := json.Unmarshal([]byte(c.PostForm("tags")), &tags); err != nil {
		jsonMsg(c, "adoptInbounds", common.NewError("invalid tags"))
		return
	}
	if err := a.NodeSyncService.AdoptInbounds(uint(id), tags, loginUser); err != nil {
		jsonMsg(c, "adoptInbounds", err)
		return
	}
	// Push the master's clients onto the freshly adopted inbounds right away.
	// ReconcileNow skips the backoff; if it still loses to an in-flight run,
	// the dirty flag set by AdoptInbounds lets the heartbeat converge instead.
	go func() {
		if err := a.NodeSyncService.ReconcileNow(uint(id)); err != nil {
			logger.Warning("adopt: initial reconcile failed: ", err)
		}
	}()
	jsonMsg(c, "adoptInbounds", nil)
}

func (a *ApiService) ReconcileNode(c *gin.Context) {
	id, err := strconv.ParseUint(c.PostForm("id"), 10, 64)
	if err != nil {
		jsonMsg(c, "reconcileNode", common.NewError("invalid node id"))
		return
	}
	err = a.NodeSyncService.ReconcileNow(uint(id))
	jsonMsg(c, "reconcileNode", err)
}
