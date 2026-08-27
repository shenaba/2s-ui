package service

import (
	"encoding/json"
	"net/netip"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/shenaba/2s-ui/config"
	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/util/common"

	"gorm.io/gorm"
)

var defaultConfig = `{
  "log": {
    "level": "info"
  },
  "dns": {
    "servers": [],
    "rules": []
  },
  "route": {
    "rules": [
		  {
        "action": "sniff"
      },
      {
        "protocol": [
          "dns"
        ],
        "action": "hijack-dns"
      }
    ]
  },
  "experimental": {}
}`

var defaultValueMap = map[string]string{
	"webListen":          "",
	"webDomain":          "",
	"webPort":            "2095",
	"secret":             common.Random(32),
	"webCertFile":        "",
	"webKeyFile":         "",
	"webCertMode":        "",
	"webNginx":           "",
	"webTrustedProxies":  "",
	"webAcmeMethod":      "auto",
	"webAcmeEmail":       "",
	"webPath":            "/app/",
	"webURI":             "",
	"sessionMaxAge":      "0",
	"loginMaxFailures":   "5",
	"loginFailWindow":    "5",
	"loginBanDuration":   "15",
	"trafficAge":         "30",
	"statsBucketSeconds": "60",
	"timeLocation":       "Asia/Tehran",
	"subListen":          "",
	"subPort":            "2096",
	"subPath":            "/sub/",
	"subDomain":          "",
	"subCertFile":        "",
	"subKeyFile":         "",
	"subCertMode":        "",
	"subNginx":           "",
	"subAcmeEmail":       "",
	"subUpdates":         "12",
	"subEncode":          "true",
	"subShowInfo":        "false",
	"subURI":             "",
	"subJsonExt":         "",
	"subClashExt":        "",
	"subClashNoDefGrp":   "false",
	"subClashSprtAll":    "false",
	"globalReset":        "",
	"globalResetLast":    "0",
	"config":             defaultConfig,
	"version":            config.GetVersion(),

	// Notifications. notifyEvents is a comma-separated list of notify.Kind
	// values -- the kinds themselves are documented in service/notify/event.go,
	// and renaming one there silently turns it off for everyone who had it on.
	//
	// The default set covers what an operator cannot find out any other way
	// without watching the panel: a node or the core going down, a client
	// running out, and someone getting locked out of the login. Successful
	// logins are on too, because a sign-in the operator did not perform is the
	// single most useful thing this can tell them. The chatty ones (every
	// individual failed login, CPU and memory thresholds) are off by default.
	"notifyEnable":       "false",
	"notifyProxy":        "",
	"notifyLang":         "en",
	"notifyEvents":       "node.down,node.up,core.crash,core.up,client.depleted,client.expiring,login.success,login.banned",
	"notifyExpireDays":   "3",
	"notifyVolumeGB":     "5",
	"notifyCpu":          "80",
	"notifyMemory":       "80",
	"notifyNodeFlap":     "3",
	"notifyTgToken":      "",
	"notifyTgChatId":     "",
	"notifyTgApiServer":  "",
	"notifyWebhookUrl":   "",
	"notifySmtpHost":     "",
	"notifySmtpPort":     "587",
	"notifySmtpUser":     "",
	"notifySmtpPass":     "",
	"notifySmtpFrom":     "",
	"notifySmtpTo":       "",
	"notifySmtpSecurity": "starttls",
	"notifyBackup":       "false",
	"notifyReport":       "",
	// Every notify* key the code reads must have a default here: notifySettings
	// seeds its map from this one and ignores DB rows for keys it does not know,
	// so a missing entry reads as the zero value forever -- and Save's UPDATE
	// touches no row, which means the settings page cannot store it either.
	"notifyBotEnable": "false",
}

// notifySecrets are the notification settings that must never be read back out
// of the panel, following the same rule as `secret`. Both are write-only in the
// UI: GetAllSetting strips them and reports a has* boolean instead, and Save
// skips them when they arrive empty so a plain save cannot wipe one.
var notifySecrets = []string{"notifyTgToken", "notifySmtpPass"}

type SettingService struct {
}

func (s *SettingService) GetAllSetting() (*map[string]string, error) {
	db := database.GetDB()
	settings := make([]*model.Setting, 0)
	err := db.Model(model.Setting{}).Find(&settings).Error
	if err != nil {
		return nil, err
	}
	allSetting := map[string]string{}

	for _, setting := range settings {
		allSetting[setting.Key] = setting.Value
	}

	for key, defaultValue := range defaultValueMap {
		if _, exists := allSetting[key]; !exists {
			err = s.saveSetting(key, defaultValue)
			if err != nil {
				return nil, err
			}
			allSetting[key] = defaultValue
		}
	}

	// Due to security principles
	delete(allSetting, "secret")
	delete(allSetting, "config")
	delete(allSetting, "version")
	// Internal bookkeeping, advanced automatically by the reset job
	delete(allSetting, "globalResetLast")

	// Notification credentials go the same way, but silently dropping them
	// would leave the settings page showing an empty field, which reads as "not
	// configured" and invites the operator to retype a token they still have.
	// Report whether one is set instead.
	for _, key := range notifySecrets {
		if allSetting[key] != "" {
			allSetting[hasKey(key)] = "true"
		}
		delete(allSetting, key)
	}

	return &allSetting, nil
}

// hasKey is the companion flag for a write-only setting: notifyTgToken ->
// hasNotifyTgToken.
func hasKey(key string) string {
	if key == "" {
		return ""
	}
	return "has" + strings.ToUpper(key[:1]) + key[1:]
}

func (s *SettingService) ResetSettings() error {
	db := database.GetDB()
	return db.Where("1 = 1").Delete(model.Setting{}).Error
}

func (s *SettingService) getSetting(key string) (*model.Setting, error) {
	db := database.GetDB()
	setting := &model.Setting{}
	err := db.Model(model.Setting{}).Where("key = ?", key).First(setting).Error
	if err != nil {
		return nil, err
	}
	return setting, nil
}

func (s *SettingService) getString(key string) (string, error) {
	setting, err := s.getSetting(key)
	if database.IsNotFound(err) {
		value, ok := defaultValueMap[key]
		if !ok {
			return "", common.NewErrorf("key <%v> not in defaultValueMap", key)
		}
		return value, nil
	} else if err != nil {
		return "", err
	}
	return setting.Value, nil
}

func (s *SettingService) saveSetting(key string, value string) error {
	setting, err := s.getSetting(key)
	db := database.GetDB()
	if database.IsNotFound(err) {
		return db.Create(&model.Setting{
			Key:   key,
			Value: value,
		}).Error
	} else if err != nil {
		return err
	}
	setting.Key = key
	setting.Value = value
	return db.Save(setting).Error
}

func (s *SettingService) setString(key string, value string) error {
	return s.saveSetting(key, value)
}

func (s *SettingService) getBool(key string) (bool, error) {
	str, err := s.getString(key)
	if err != nil {
		return false, err
	}
	return strconv.ParseBool(str)
}

// func (s *SettingService) setBool(key string, value bool) error {
// 	return s.setString(key, strconv.FormatBool(value))
// }

func (s *SettingService) getInt(key string) (int, error) {
	str, err := s.getString(key)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(str)
}

func (s *SettingService) setInt(key string, value int) error {
	return s.setString(key, strconv.Itoa(value))
}
func (s *SettingService) GetListen() (string, error) {
	return s.getString("webListen")
}

func (s *SettingService) GetWebDomain() (string, error) {
	return s.getString("webDomain")
}

func (s *SettingService) GetPort() (int, error) {
	return s.getInt("webPort")
}

func (s *SettingService) SetPort(port int) error {
	return s.setInt("webPort", port)
}

func (s *SettingService) GetCertFile() (string, error) {
	return s.getString("webCertFile")
}

func (s *SettingService) GetKeyFile() (string, error) {
	return s.getString("webKeyFile")
}

func (s *SettingService) GetWebCertMode() (string, error) {
	return s.getString("webCertMode")
}

func (s *SettingService) GetWebNginx() (bool, error) {
	// 空字符串表示"尚未设置/未部署",安全地按 false 处理(避免 ParseBool("") 报错)。
	// 读失败则必须传出去、不能一样塌成 false:那会让启动时的对账把「读不出来」当成
	// 「用户关掉了」,删掉正在服务的 443 入口(详见 ProxyVhostSpecs)。
	v, err := s.getString("webNginx")
	if err != nil {
		return false, err
	}
	if v == "" {
		return false, nil
	}
	return strconv.ParseBool(v)
}

// GetWebTrustedProxies returns the peers that are allowed to speak for a
// caller through X-Forwarded-For. This is independent of webNginx: operators
// using their own nginx, Caddy, tunnel or load balancer still need correct
// source addresses without asking the panel to manage that proxy.
func (s *SettingService) GetWebTrustedProxies() ([]netip.Prefix, error) {
	raw, err := s.getString("webTrustedProxies")
	if err != nil {
		return nil, err
	}
	return parseTrustedProxies(raw)
}

func parseTrustedProxies(raw string) ([]netip.Prefix, error) {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	prefixes := make([]netip.Prefix, 0, len(parts))
	for _, part := range parts {
		prefix, err := netip.ParsePrefix(part)
		if err != nil {
			addr, addrErr := netip.ParseAddr(part)
			if addrErr != nil {
				return nil, common.NewErrorf("invalid trusted proxy %q: expected an IP address or CIDR", part)
			}
			addr = addr.Unmap()
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func (s *SettingService) GetWebAcmeEmail() (string, error) {
	return s.getString("webAcmeEmail")
}

// GetWebURI 返回面板对外地址的手工覆盖值(空表示未设置,由调用方自行推断)。
// 反代场景下面板推断不出对外地址,只能靠它,参见 sui uri 与前端 restartApp。
func (s *SettingService) GetWebURI() (string, error) {
	return s.getString("webURI")
}

func (s *SettingService) GetWebPath() (string, error) {
	webPath, err := s.getString("webPath")
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(webPath, "/") {
		webPath = "/" + webPath
	}
	if !strings.HasSuffix(webPath, "/") {
		webPath += "/"
	}
	return webPath, nil
}

func (s *SettingService) SetWebPath(webPath string) error {
	if !strings.HasPrefix(webPath, "/") {
		webPath = "/" + webPath
	}
	if !strings.HasSuffix(webPath, "/") {
		webPath += "/"
	}
	return s.setString("webPath", webPath)
}

func (s *SettingService) GetSecret() ([]byte, error) {
	secret, err := s.getString("secret")
	if secret == defaultValueMap["secret"] {
		err := s.saveSetting("secret", secret)
		if err != nil {
			logger.Warning("save secret failed:", err)
		}
	}
	return []byte(secret), err
}

func (s *SettingService) GetSessionMaxAge() (int, error) {
	return s.getInt("sessionMaxAge")
}

func (s *SettingService) GetTrafficAge() (int, error) {
	return s.getInt("trafficAge")
}

// GetLoginGuard returns the login rate limit's three knobs: how many failures
// are tolerated, over how many minutes, and how many minutes a ban then lasts.
// Zero or negative failures disables the limiter outright, which is why the
// caller gets the raw numbers rather than a "enabled" flag -- see loginGuard.
func (s *SettingService) GetLoginGuard() (maxFailures int, windowMinutes int, banMinutes int, err error) {
	if maxFailures, err = s.getInt("loginMaxFailures"); err != nil {
		return 0, 0, 0, err
	}
	if windowMinutes, err = s.getInt("loginFailWindow"); err != nil {
		return 0, 0, 0, err
	}
	if banMinutes, err = s.getInt("loginBanDuration"); err != nil {
		return 0, 0, 0, err
	}
	return maxFailures, windowMinutes, banMinutes, nil
}

// GetStatsBucketSeconds returns the bucket size (in seconds) that traffic
// samples are rounded down to before being stored. Larger buckets mean fewer
// rows at the cost of chart resolution. Falls back to the default on a missing
// or non-positive value.
func (s *SettingService) GetStatsBucketSeconds() (int64, error) {
	v, err := s.getInt("statsBucketSeconds")
	if err != nil {
		return 0, err
	}
	if v < 1 {
		def, _ := strconv.Atoi(defaultValueMap["statsBucketSeconds"])
		return int64(def), nil
	}
	return int64(v), nil
}

func (s *SettingService) GetTimeLocation() (*time.Location, error) {
	l, err := s.getString("timeLocation")
	if err != nil {
		return nil, err
	}
	if runtime.GOOS == "windows" {
		l = "Local"
	}
	location, err := time.LoadLocation(l)
	if err != nil {
		defaultLocation := defaultValueMap["timeLocation"]
		logger.Errorf("location <%v> not exist, using default location: %v", l, defaultLocation)
		return time.LoadLocation(defaultLocation)
	}
	return location, nil
}

func (s *SettingService) GetSubListen() (string, error) {
	return s.getString("subListen")
}

func (s *SettingService) GetSubPort() (int, error) {
	return s.getInt("subPort")
}

func (s *SettingService) SetSubPort(subPort int) error {
	return s.setInt("subPort", subPort)
}

func (s *SettingService) GetSubPath() (string, error) {
	subPath, err := s.getString("subPath")
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(subPath, "/") {
		subPath = "/" + subPath
	}
	if !strings.HasSuffix(subPath, "/") {
		subPath += "/"
	}
	return subPath, nil
}

func (s *SettingService) SetSubPath(subPath string) error {
	if !strings.HasPrefix(subPath, "/") {
		subPath = "/" + subPath
	}
	if !strings.HasSuffix(subPath, "/") {
		subPath += "/"
	}
	return s.setString("subPath", subPath)
}

func (s *SettingService) GetSubDomain() (string, error) {
	return s.getString("subDomain")
}

func (s *SettingService) GetSubCertFile() (string, error) {
	return s.getString("subCertFile")
}

func (s *SettingService) GetSubKeyFile() (string, error) {
	return s.getString("subKeyFile")
}

func (s *SettingService) GetSubCertMode() (string, error) {
	return s.getString("subCertMode")
}

// GetSubNginx 是订阅侧的「由反向代理终结 TLS」,语义与 webNginx 对称:
// 开着时订阅服务只跑明文 HTTP,TLS 交给前面的 nginx。
// 空字符串表示尚未设置,按 false 处理(避免 ParseBool("") 报错);读失败按 GetWebNginx
// 同样的理由往上传,不塌成 false。
func (s *SettingService) GetSubNginx() (bool, error) {
	v, err := s.getString("subNginx")
	if err != nil {
		return false, err
	}
	if v == "" {
		return false, nil
	}
	return strconv.ParseBool(v)
}

// ProxyVhostSpecs derives which reverse-proxy vhosts nginx should have from the
// settings ALREADY PERSISTED in the DB, for the startup reconciliation in
// app.syncNginxProxy. It shares BuildVhostSpecs with the API path that reads the
// form; the two must produce identical results.
//
// Every read error is reported rather than defaulted away: the reconciliation
// deletes any generated vhost whose side comes back disabled, so collapsing a failed
// read to Enabled=false would tear down a live 443 entrypoint over a transient DB
// error. Every key here is in defaultValueMap, so a missing row yields the default —
// reaching this path means the read itself failed.
func (s *SettingService) ProxyVhostSpecs() ([]ProxySide, error) {
	var firstErr error
	keep := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	get := func(f func() (string, error)) string { v, err := f(); keep(err); return v }
	getInt := func(f func() (int, error)) int { v, err := f(); keep(err); return v }
	getBool := func(f func() (bool, error)) bool { v, err := f(); keep(err); return v }

	sides := []ProxySide{
		{
			Name: "panel", Enabled: getBool(s.GetWebNginx), Domain: get(s.GetWebDomain),
			Path: get(s.GetWebPath), Listen: get(s.GetListen), Port: getInt(s.GetPort),
			CertFile: get(s.GetCertFile), KeyFile: get(s.GetKeyFile),
		},
		{
			Name: "subscription", Enabled: getBool(s.GetSubNginx), Domain: get(s.GetSubDomain),
			Path: get(s.GetSubPath), Listen: get(s.GetSubListen), Port: getInt(s.GetSubPort),
			CertFile: get(s.GetSubCertFile), KeyFile: get(s.GetSubKeyFile),
		},
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return sides, nil
}

func (s *SettingService) GetSubAcmeEmail() (string, error) {
	return s.getString("subAcmeEmail")
}

func (s *SettingService) GetSubUpdates() (int, error) {
	return s.getInt("subUpdates")
}

func (s *SettingService) GetSubEncode() (bool, error) {
	return s.getBool("subEncode")
}

func (s *SettingService) GetSubShowInfo() (bool, error) {
	return s.getBool("subShowInfo")
}

func (s *SettingService) GetSubURI() (string, error) {
	return s.getString("subURI")
}

// GetGlobalReset returns the configured period for resetting all clients'
// traffic: "off", "weekly", "monthly" or "yearly".
func (s *SettingService) GetGlobalReset() (string, error) {
	return s.getString("globalReset")
}

// GetGlobalResetLast returns the unix time of the last global traffic reset.
func (s *SettingService) GetGlobalResetLast() (int64, error) {
	str, err := s.getString("globalResetLast")
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(str, 10, 64)
}

func (s *SettingService) SetGlobalResetLast(value int64) error {
	return s.setString("globalResetLast", strconv.FormatInt(value, 10))
}

func (s *SettingService) GetFinalSubURI(host string) (string, error) {
	allSetting, err := s.GetAllSetting()
	if err != nil {
		return "", err
	}
	SubURI := (*allSetting)["subURI"]
	if SubURI != "" {
		return SubURI, nil
	}
	// TLS 判定必须与 sub.go 的实际行为一致【且按同样的优先级】:subNginx 最先短路——
	// 订阅自身跑明文,对外由 nginx 在 443 说 https,链接里既不能是 http 也不能带订阅
	// 自身的内网端口;其后才是 acme 模式,或证书/私钥【任一】非空即尝试 TLS。早先这里
	// 要求两者都填,只填一个时 sub 服务已经在跑 https,本函数生成的订阅链接却还是
	// http——而这是真正发给客户端的地址。判定与 sui uri 同源。
	subNginx := runtime.GOOS != "windows" && (*allSetting)["subNginx"] == "true"
	protocol := "http"
	if subNginx || (*allSetting)["subCertMode"] == "acme" ||
		(*allSetting)["subKeyFile"] != "" || (*allSetting)["subCertFile"] != "" {
		protocol = "https"
	}
	if (*allSetting)["subDomain"] != "" {
		host = (*allSetting)["subDomain"]
	}
	// 协议默认端口不写进 URL。注意必须【先比较后拼冒号】:早先写成 ":"+port 再跟
	// "80"/"443" 比,永远不相等,省略逻辑是死代码,链接一直带着 :80 / :443。
	// 判定顺序与前端 buildURL 保持一致。
	port := (*allSetting)["subPort"]
	if subNginx || port == "" || (protocol == "http" && port == "80") || (protocol == "https" && port == "443") {
		port = ""
	} else {
		port = ":" + port
	}
	return protocol + "://" + host + port + (*allSetting)["subPath"], nil
}

func (s *SettingService) GetConfig() (string, error) {
	return s.getString("config")
}

func (s *SettingService) SetConfig(config string) error {
	return s.setString("config", config)
}

func (s *SettingService) SaveConfig(tx *gorm.DB, config json.RawMessage) error {
	configs, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return tx.Model(model.Setting{}).Where("key = ?", "config").Update("value", string(configs)).Error
}

func (s *SettingService) Save(tx *gorm.DB, data json.RawMessage) error {
	var err error
	var settings map[string]string
	err = json.Unmarshal(data, &settings)
	if err != nil {
		return err
	}
	// When ACME auto-cert is enabled the manual cert/key paths are unused (and
	// may be stale/deleted), so skip their file-existence check below.
	// nginx 模式下面板自身只跑 HTTP,web 侧 cert/key 同样不使用,也跳过检查
	// (此时证书可能尚未申请,路径还不存在)。
	webAcme := settings["webCertMode"] == "acme"
	subAcme := settings["subCertMode"] == "acme"
	webNginx := settings["webNginx"] == "true"
	subNginx := settings["subNginx"] == "true"
	for key, obj := range settings {
		// A write-only credential arrives empty on every save that did not
		// retype it -- GetAllSetting never sent the stored value out, so the
		// form has nothing to send back. Writing that through would clear the
		// token the first time the operator changed any other field on the
		// page. Empty means "leave it alone"; to change one, type the new
		// value.
		if obj == "" && isNotifySecret(key) {
			continue
		}
		if isNotifySecretFlag(key) {
			continue
		}
		if key == "webTrustedProxies" {
			if _, err = parseTrustedProxies(obj); err != nil {
				return err
			}
		}
		// Secure file existence check
		if obj != "" &&
			(((key == "webCertFile" || key == "webKeyFile") && !webAcme && !webNginx) ||
				((key == "subCertFile" || key == "subKeyFile") && !subAcme && !subNginx)) {
			err = s.fileExists(obj)
			if err != nil {
				return common.NewError(" -> ", obj, " is not exists")
			}
		}

		// 域名会进 nginx 的 server_name、Host 校验和证书目录名,通配符在这些位置全都
		// 不成立——DomainValidator 按 Host 逐字比对,「*.example.com」永远匹配不上,
		// 面板会整个 403,而 sui setting 没有改域名的 flag,只能重置或改库才能救回。
		if (key == "webDomain" || key == "subDomain") && strings.Contains(obj, "*") {
			return common.NewErrorf("%s 不能是通配符域名: %q;请填一个具体的主机名(通配符证书对具体主机名同样生效)", key, obj)
		}

		// Correct Pathes start and ends with `/`
		if key == "webPath" ||
			key == "subPath" {
			if !strings.HasPrefix(obj, "/") {
				obj = "/" + obj
			}
			if !strings.HasSuffix(obj, "/") {
				obj += "/"
			}
		}

		// Delete all stats if it is set to 0
		if key == "trafficAge" && obj == "0" {
			err = tx.Where("id > 0").Delete(model.Stats{}).Error
			if err != nil {
				return err
			}
		}
		err = tx.Model(model.Setting{}).Where("key = ?", key).Update("value", obj).Error
		if err != nil {
			return err
		}
	}
	return err
}

func (s *SettingService) GetSubJsonExt() (string, error) {
	return s.getString("subJsonExt")
}

func (s *SettingService) GetSubClashExt() (string, error) {
	return s.getString("subClashExt")
}

// GetSubClashNoDefGrp reports whether the default "Proxy"/"Auto" proxy-groups
// should never be injected into a Clash subscription. When true, the config is
// left with exactly the groups the user defined.
func (s *SettingService) GetSubClashNoDefGrp() (bool, error) {
	return s.getBool("subClashNoDefGrp")
}

// GetSubClashSprtAll reports whether a case-insensitive "all" entry inside a
// custom proxy-group's "proxies" list should be expanded into every generated
// proxy tag.
func (s *SettingService) GetSubClashSprtAll() (bool, error) {
	return s.getBool("subClashSprtAll")
}

func (s *SettingService) fileExists(path string) error {
	_, err := os.Stat(path)
	return err
}
