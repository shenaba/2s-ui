package app

import (
	"log"

	"github.com/shenaba/2s-ui/config"
	"github.com/shenaba/2s-ui/core"
	"github.com/shenaba/2s-ui/cronjob"
	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service"
	"github.com/shenaba/2s-ui/sub"
	"github.com/shenaba/2s-ui/web"

	"github.com/op/go-logging"
)

type APP struct {
	service.SettingService
	configService *service.ConfigService
	webServer     *web.Server
	subServer     *sub.Server
	cronJob       *cronjob.CronJob
	logger        *logging.Logger
	core          *core.Core
}

func NewApp() *APP {
	return &APP{}
}

func (a *APP) Init() error {
	log.Printf("%v %v", config.GetName(), config.GetVersion())

	a.initLog()

	err := database.InitDB(config.GetDBPath())
	if err != nil {
		return err
	}

	// Init Setting
	a.SettingService.GetAllSetting()

	// 升级上来的实例:把手填在设置里的证书路径归档成一条可管理的证书记录。
	// 幂等,且不改设置本身,所以每次启动跑一遍是安全的。
	var certService service.CertService
	certService.ArchiveLegacy()

	a.core = core.NewCore()

	a.cronJob = cronjob.NewCronJob()
	a.webServer = web.NewServer()
	a.subServer = sub.NewServer()

	a.configService = service.NewConfigService(a.core)

	return nil
}

func (a *APP) Start() error {
	loc, err := a.SettingService.GetTimeLocation()
	if err != nil {
		return err
	}

	trafficAge, err := a.SettingService.GetTrafficAge()
	if err != nil {
		return err
	}

	statsBucketSeconds, err := a.SettingService.GetStatsBucketSeconds()
	if err != nil {
		return err
	}

	globalReset, err := a.SettingService.GetGlobalReset()
	if err != nil {
		return err
	}

	err = a.cronJob.Start(loc, trafficAge, statsBucketSeconds, globalReset)
	if err != nil {
		return err
	}

	err = a.webServer.Start()
	if err != nil {
		return err
	}

	err = a.subServer.Start()
	if err != nil {
		return err
	}

	err = a.configService.StartCore()
	if err != nil {
		logger.Error(err)
	}

	a.syncNginxProxy()

	return nil
}

// syncNginxProxy aligns the auto-generated reverse-proxy configs in nginx with
// the settings ALREADY PERSISTED in the DB.
//
// Runs after both servers are up: by then the panel has decided, per the new
// settings, whether it serves plaintext or TLS, so touching nginx now cannot open
// a window where neither side is serving.
//
// Turning the proxy OFF can only be finished here. The frontend dares not touch
// nginx while switching the panel side off — the current page is served over the
// 443 that vhost provides, and the moment it is deleted the follow-up api/save and
// api/restartApp can no longer be sent. The result would be "vhost gone, settings
// unsaved": the panel keeps serving plaintext while nobody answers on 443, locking
// the user out. So that step waits until after the restart and happens here, once
// the panel terminates TLS itself again.
//
// It also heals two classes of leftovers: pre-rename s-ui-panel-*.conf files, and
// orphans left behind when the proxy was switched off without cleanup.
// With the proxy on and nothing changed, SyncVhosts short-circuits and will not
// reload nginx for nothing.
// Failures are logged only: whether nginx is installed, or whether the panel runs
// as root, must never block startup.
func (a *APP) syncNginxProxy() {
	var acme service.AcmeService
	specs := service.BuildVhostSpecs(a.SettingService.ProxyVhostSpecs()...)
	if _, err := acme.SyncVhosts(specs); err != nil {
		logger.Warning("启动时对账 nginx 反代配置失败(不影响面板运行):", err)
	}
}

func (a *APP) Stop() {
	a.cronJob.Stop()
	err := a.subServer.Stop()
	if err != nil {
		logger.Warning("stop Sub Server err:", err)
	}
	err = a.webServer.Stop()
	if err != nil {
		logger.Warning("stop Web Server err:", err)
	}
	err = a.configService.StopCore()
	if err != nil {
		logger.Warning("stop Core err:", err)
	}
}

func (a *APP) initLog() {
	switch config.GetLogLevel() {
	case config.Debug:
		logger.InitLogger(logging.DEBUG)
	case config.Info:
		logger.InitLogger(logging.INFO)
	case config.Warn:
		logger.InitLogger(logging.WARNING)
	case config.Error:
		logger.InitLogger(logging.ERROR)
	default:
		log.Fatal("unknown log level:", config.GetLogLevel())
	}
}

func (a *APP) RestartApp() {
	a.Stop()
	a.Start()
}

func (a *APP) GetCore() *core.Core {
	return a.core
}
