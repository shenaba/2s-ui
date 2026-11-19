package cronjob

import "github.com/shenaba/2s-ui/service"

// AcmeRenewJob 兜底续期 acme.sh 签发的证书(容器里没有 crond,见 AcmeService.RenewDue)。
// 每小时检查一次而不是 @daily:一来面板重启频繁时 @every 24h 可能永远等不到触发,
// 二来 @daily 会让所有实例在同一个零点去打 Let's Encrypt。没有到期证书时只读几个本地
// 文件;真正运行 acme.sh 的频率由 RenewDue 限制在每天至多一次。
type AcmeRenewJob struct {
	service.AcmeService
}

func NewAcmeRenewJob() *AcmeRenewJob {
	return &AcmeRenewJob{}
}

func (j *AcmeRenewJob) Run() {
	j.AcmeService.RenewDue()
}
