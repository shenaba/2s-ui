package cronjob

import (
	"fmt"
	"hash/fnv"

	"github.com/shenaba/2s-ui/service"
	"github.com/shenaba/2s-ui/service/notify"
)

// AcmeRenewJob 兜底续期 acme.sh 签发的证书(容器里没有 crond,见 AcmeService.RenewDue)。
// 没有到期证书时只读几个本地文件;真正运行 acme.sh 的频率由 RenewDue 限制在每天至多一次。
type AcmeRenewJob struct {
	service.AcmeService
}

func NewAcmeRenewJob() *AcmeRenewJob {
	return &AcmeRenewJob{}
}

func (j *AcmeRenewJob) Run() {
	j.AcmeService.RenewDue()
}

// acmeRenewSpec 让续期检查每小时在一个固定的分钟跑,而不是 @every 1h。
//
// @every 从 cron 启动那一刻起算,而每次保存设置、点重启、导入数据库都会经 SIGHUP 重建
// cron,计时随之归零:面板重启比每小时还勤时,这个任务一次也轮不到(不用 @every 24h
// 的理由在这里同样成立,只是尺度小一些)。挂钟上的固定分钟不受重启影响。
//
// 分钟由 seed 散列得出,seed 应当每个安装各不相同、且重启前后不变:同一个安装每次启动
// 都落在同一分钟,重启不会让某一小时被跳过;不同安装则错开,不会像 @hourly 那样一起在
// 整点去打 Let's Encrypt。不能用主机名:docker-compose.yml 写死了 hostname: "s-ui",
// 所有 compose 部署会落在同一分钟。调用方传 SettingService.GetInstallId(每个安装
// 随机生成并落库的标识,专为这类用途设,不拿会话密钥凑数)。
// seed 为空时退回主机名(notify.Host,与通知标题里的是同一个)。
func acmeRenewSpec(seed string) string {
	if seed == "" {
		seed = notify.Host()
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	return fmt.Sprintf("%d * * * *", h.Sum32()%60)
}
