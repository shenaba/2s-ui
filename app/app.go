package app

import (
	"log"

	"github.com/shenaba/2s-ui/cmd/migration"
	"github.com/shenaba/2s-ui/config"
	"github.com/shenaba/2s-ui/core"
	"github.com/shenaba/2s-ui/cronjob"
	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service"
	"github.com/shenaba/2s-ui/service/notify"
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

	// Data repairs from older releases run here, before the panel opens the
	// database, on every start-up. The in-panel update button swaps the binary
	// and restarts the service without ever calling `sui migrate`, so leaving
	// this to the install script left every panel-updated instance unmigrated.
	// Each restart path — self-update, systemd, Docker, manual — ends up
	// executing this binary, so this one call covers all of them. A failure is
	// logged and start-up continues: a data repair that cannot be applied must
	// not keep the panel from booting.
	if err := migration.MigrateDbQuietly(); err != nil {
		logger.Warning("database migration failed: ", err)
	}

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

	// The hub must exist before the cron jobs first fire into it and before
	// the web server accepts the first upgrade.
	service.StartHub()

	// Notifications come up alongside the hub and before the cron jobs, so the
	// very first node probe or core start already has somewhere to report to.
	// GetNotifyConfig is passed as a method value rather than a snapshot: it is
	// called per event, which is what makes a settings change take effect
	// without a restart.
	notify.Start(a.SettingService.GetNotifyConfig)

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

// syncNginxProxy aligns the auto-generated reverse-proxy configs in nginx with the
// settings ALREADY PERSISTED in the DB. It runs after both servers are up, so the
// panel has already decided whether it serves plaintext or TLS and touching nginx
// cannot open a window where neither side is serving.
//
// Turning the proxy OFF can only be finished here. The frontend dares not delete the
// vhost while switching the panel side off — that vhost is the 443 the current page
// arrives on, so the follow-up api/save and api/restartApp would never be sent, and
// the panel would end up plaintext with nobody answering on 443. It also heals
// pre-rename s-ui-panel-*.conf files and orphans left by an earlier switch-off.
//
// Failures are logged only: whether nginx is installed, or whether the panel runs as
// root, must never block startup.
func (a *APP) syncNginxProxy() {
	sides, err := a.SettingService.ProxyVhostSpecs()
	if err != nil {
		// Never reconcile on a failed read: the side comes back disabled and SyncVhosts
		// would delete a 443 entrypoint that is serving fine. Doing nothing is recoverable.
		logger.Warning("读取反代设置失败,跳过启动时的 nginx 对账(nginx 保持原样):", err)
		return
	}
	specs, err := service.BuildVhostSpecs(sides...)
	if err != nil {
		// 典型是「开关开着但域名空」这种半成品状态。同样不能继续:那一侧被跳过后 specs
		// 会少一份甚至变空,对账随即把已生成的 vhost 当成多余的删掉。
		logger.Warning("反代设置不完整,跳过启动时的 nginx 对账(nginx 保持原样):", err)
		return
	}
	var acme service.AcmeService
	if _, err := acme.SyncVhosts(specs); err != nil {
		logger.Warning("启动时对账 nginx 反代配置失败(不影响面板运行):", err)
	}
}

func (a *APP) Stop() {
	// cron.Stop does not wait for in-flight jobs; every hub entry point is a
	// safe no-op once StopHub has swapped the singleton out.
	a.cronJob.Stop()
	err := a.subServer.Stop()
	if err != nil {
		logger.Warning("stop Sub Server err:", err)
	}
	err = a.webServer.Stop()
	if err != nil {
		logger.Warning("stop Web Server err:", err)
	}
	// After webServer.Stop: the listener is closed, so no new handshake can
	// race the teardown. Shutdown ignores hijacked connections — closing the
	// live sockets is the hub's job, or every restart leaks them.
	service.StopHub()
	// After the servers and before the core: a core teardown that reports a
	// failure should still find the notifier up.
	notify.Stop()
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
