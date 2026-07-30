package service

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/util/common"
)

// 证书申请统一走系统 acme.sh,证书装到与 s-ui.sh 脚本一致的 /root/cert/{域名}/,
// 便于脚本 / nginx / sing-box 按固定文件名(fullchain.pem / privkey.pem)复用。
const (
	certBaseDir   = "/root/cert"        // 与脚本完全一致
	nginxConfDir  = "/etc/nginx/conf.d" // 自动生成的 ACME 验证 server 块所在目录
	acmeIssueTO   = 180 * time.Second   // 申请/安装超时
	acmeInstallTO = 120 * time.Second   // acme.sh / socat 安装超时
	cmdDetectTO   = 5 * time.Second     // 检测类命令超时
	// systemd 服务的 PATH 可能不全(且不继承登录 shell),补一个兜底,
	// 确保 exec 调用能定位 nginx/socat/systemctl/apt 等。
	fallbackPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
)

// 验证方式:standalone 独占 80 端口;nginx 借用运行中的 nginx,不中断 80 端口服务。
const (
	methodStandalone = "standalone"
	methodNginx      = "nginx"
)

// acmeIssuing 保证同一时刻只有一个证书申请在跑,避免前端重复点击导致并发申请、
// 撞 Let's Encrypt 限速。
var acmeIssuing sync.Mutex

// AcmeService 是无状态工具,不嵌入 SettingService(避免与 ApiService 已嵌入的
// SettingService 产生方法集二义性)。所有入参由调用方传入,不直接读写数据库。
type AcmeService struct{}

type NginxStatus struct {
	Installed  bool `json:"installed"`
	Active     bool `json:"active"`
	Port80Busy bool `json:"port80Busy"`
}

type IssueResult struct {
	CertFile  string `json:"certFile"`
	KeyFile   string `json:"keyFile"`
	Method    string `json:"method"`    // 实际使用的验证方式:standalone / nginx
	ReloadCmd string `json:"reloadCmd"` // 续期后的重载命令,空表示没配(前端据此提示自配钩子)
}

// withHome 返回把 HOME 固定为指定值、并补全 PATH 兜底的环境变量(去重)。
// systemd 服务即使以 root 运行也往往不设 HOME、PATH 也可能不全。
func withHome(home string) []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+2)
	hasPath := false
	for _, e := range env {
		switch {
		case strings.HasPrefix(e, "HOME="):
			continue
		case strings.HasPrefix(e, "PATH="):
			hasPath = true
			e = e + ":" + fallbackPath // 合并兜底路径,重复项无害
		}
		out = append(out, e)
	}
	out = append(out, "HOME="+home)
	if !hasPath {
		out = append(out, "PATH="+fallbackPath)
	}
	return out
}

// resolveBin 定位外部命令:先按进程自身 PATH 找(exec 定位二进制用的是进程 PATH,
// withHome 注入的兜底 PATH 只对子进程内部生效,如 acme.sh 再调 socat),找不到再扫
// fallbackPath,保证极简 PATH 的服务环境下也能找到 nginx/systemctl/apt 等。
func resolveBin(name string) (string, bool) {
	if p, err := exec.LookPath(name); err == nil {
		return p, true
	}
	for _, dir := range strings.Split(fallbackPath, ":") {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0111 != 0 {
			return p, true
		}
	}
	return name, false // 原样返回,让 exec 报标准的 not found 错误
}

// runCmd 执行外部命令(HOME 固定为 home),合并 stdout/stderr,超时或非零退出码
// 都包成 error,并把输出原文附在错误里回传前端,便于排查(80 端口被占、域名未解析等)。
func runCmd(timeout time.Duration, home, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	bin, _ := resolveBin(name)
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = withHome(home)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))

	if ctx.Err() == context.DeadlineExceeded {
		return output, common.NewErrorf("命令超时(%v): %s %s\n%s", timeout, name, strings.Join(args, " "), output)
	}
	if err != nil {
		return output, common.NewErrorf("命令执行失败: %s %s\n%s\n%v", name, strings.Join(args, " "), output, err)
	}
	return output, nil
}

// resolveAcmeSh 在常见位置查找已安装的 acme.sh,返回可执行路径及其对应的 HOME。
// systemd 服务下 HOME 常为空,acme.sh 会装到 /.acme.sh;s-ui.sh 脚本则装到
// /root/.acme.sh。都纳入探测,避免硬编码单一路径导致"找不到"。
func resolveAcmeSh() (bin, home string) {
	candidates := make([]string, 0, 3)
	if h := os.Getenv("HOME"); h != "" {
		candidates = append(candidates, filepath.Join(h, ".acme.sh", "acme.sh"))
	}
	candidates = append(candidates, "/root/.acme.sh/acme.sh", "/.acme.sh/acme.sh")
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			// home = 可执行文件上两级目录:/x/.acme.sh/acme.sh -> /x
			return p, filepath.Dir(filepath.Dir(p))
		}
	}
	return "", ""
}

// port80Free 探测本机 80 端口是否空闲(standalone 验证需要独占 80)。
func port80Free() bool {
	ln, err := net.Listen("tcp", ":80")
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// port80Hint 在面板非 root 运行时补一句说明:非特权进程绑 :80 拿到的是 EACCES,
// 在 port80Free 里与「端口被占用」同样是 false,错误文案不区分会把排查方向带偏。
func port80Hint() string {
	if os.Geteuid() != 0 {
		return "(注意:面板当前不是以 root 运行,绑定 80 端口会被内核直接拒绝,这也可能是本次判定不可用的真正原因)"
	}
	return ""
}

// ipv6Available 探测内核能否创建 IPv6 监听(ipv6.disable=1 的主机不行):
// 生成 nginx 验证块时据此决定是否加 listen [::]:80,避免 reload 因开不了 v6 socket 失败。
func ipv6Available() bool {
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// hasGlobalIPv4 判断主机是否拥有全局 IPv4 地址(含 NAT 后的私网地址,与 ip -4 addr
// show scope global 语义一致,只排除回环与链路本地)。
// 用途:acme.sh 的 standalone 服务器默认只绑 IPv4,而 --listen-v6 是【排他】的
// (v6-only),双栈主机加上它反而会让 A 记录指向的 v4 验证失败——故仅在纯 IPv6 主机
// 上才加该标志。探测失败按「有 v4」处理:宁可维持默认,也不要把好用的双栈主机弄挂。
func hasGlobalIPv4() bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return true
	}
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		if ipn.IP.To4() != nil && !ipn.IP.IsLoopback() && !ipn.IP.IsLinkLocalUnicast() {
			return true
		}
	}
	return false
}

// validDomain 只放行主机名合法字符:域名会拼进文件路径与外部命令参数,严格校验兜底。
func validDomain(d string) bool {
	if len(d) == 0 || len(d) > 253 || d[0] == '-' || d[0] == '.' {
		return false
	}
	for _, r := range d {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-':
		default:
			return false
		}
	}
	return true
}

// validCertDomain 在 validDomain 基础上放行通配符(*.example.com):acme.sh 里可能躺着
// DNS-01 签的通配符证书,它们必须能被列出、登记和删除,否则就是页面上一行永远管不了的
// 僵尸。但 server_name / webDomain / 申请入口仍用 validDomain——通配符在那些位置不成立。
func validCertDomain(d string) bool {
	if strings.HasPrefix(d, "*.") {
		return validDomain(d[2:])
	}
	return validDomain(d)
}

// DetectNginx 检测 nginx 是否安装并运行,以及 80 端口是否被占用。Windows 直接返回零值。
func (a *AcmeService) DetectNginx() NginxStatus {
	status := NginxStatus{}
	if runtime.GOOS == "windows" {
		return status
	}
	status.Port80Busy = !port80Free()
	if _, ok := resolveBin("nginx"); ok {
		status.Installed = true
	}
	// systemctl is-active 在运行时退出码为 0、输出 "active"。
	// 局限:无 systemd 的环境(如容器里直跑 nginx)检不出 active,auto 只会走 standalone/报错。
	if out, err := runCmd(cmdDetectTO, "/root", "systemctl", "is-active", "nginx"); err == nil && strings.TrimSpace(out) == "active" {
		status.Active = true
		status.Installed = true
	}
	return status
}

// resolveMethod 把前端传入的验证方式解析为实际可执行的方式并校验可行性。
//   - standalone:需独占 80 端口。
//   - nginx     :需 nginx 正在运行(不中断 80 端口服务)。
//   - auto / 空 :80 空闲优先 standalone;80 被占且 nginx 在跑则借用 nginx;否则报错。
func (a *AcmeService) resolveMethod(method string) (string, error) {
	switch method {
	case methodStandalone:
		if !port80Free() {
			return "", common.NewErrorf("80 端口被占用,无法用 standalone 申请;若 nginx 正在运行请改选 nginx 验证或「自动」,否则请先停止占用 80 端口的服务%s", port80Hint())
		}
		return methodStandalone, nil
	case methodNginx:
		if !a.DetectNginx().Active {
			return "", common.NewError("未检测到正在运行的 nginx,无法用 nginx 验证;请先启动 nginx,或改选「自动」")
		}
		return methodNginx, nil
	case "", "auto":
		if port80Free() {
			return methodStandalone, nil
		}
		if a.DetectNginx().Active {
			return methodNginx, nil
		}
		return "", common.NewErrorf("80 端口不可用且未检测到运行中的 nginx:请停止占用 80 端口的程序,或启动 nginx 后重试%s", port80Hint())
	default:
		return "", common.NewErrorf("未知的验证方式: %q", method)
	}
}

// nginxHasServerName reports whether a chunk of nginx config declares the domain
// as a server_name. Parsed line by line, so the legal form where the value wraps
// onto its own line is missed — the result is one redundant block. That is not
// free: a duplicate name on the same port makes nginx drop whichever block it
// reads second, which is exactly what EnsureVhost rolls back on (see its
// conflicting handling). A duplicate on :80 does not affect the proxy, but can
// make acme.sh --nginx pick the wrong block.
//
// It looks only at server_name, never at listen, so it cannot answer "is there a
// block for this domain on port N" — use nginxFilesServing(dump, domain, port).
func nginxHasServerName(conf, domain string) bool {
	const directive = "server_name"
	for _, line := range strings.Split(conf, "\n") {
		line = strings.TrimSpace(line)
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		if !strings.HasPrefix(line, directive) {
			continue
		}
		rest := line[len(directive):]
		if rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
			continue // 跳过 server_names_hash_bucket_size 等同前缀指令
		}
		for _, name := range strings.Fields(strings.TrimSuffix(strings.TrimSpace(rest), ";")) {
			if strings.EqualFold(name, domain) {
				return true
			}
		}
	}
	return false
}

// ensureNginxServerBlock 确保 nginx 配置里有 server_name 匹配该域名的 server 块——
// acme.sh --nginx 靠它定位要临时改写的配置,缺失会报 "Can not find conf file"。
// 缺失时生成自包含的最小验证块(只加自己的文件,不碰用户已有配置):nginx -t 通过才
// reload,失败立即回滚删除;文件常驻,以后自动续期仍靠它验证。
func (a *AcmeService) ensureNginxServerBlock(domain string) error {
	// The check must be port-aware. Matching server_name alone lets the reverse
	// proxy's own s-ui-proxy-<domain>.conf pose as the validation block: it listens
	// on 443 only, while the HTTP-01 challenge always arrives on 80 first and gets
	// served by default_server, i.e. a 404. acme.sh --nginx ignores listen too, so
	// it injects the challenge location into that 443-only block. Typical way in:
	// the cert was issued standalone or registered by hand (no :80 block ever
	// generated) -> proxy switched on -> next issue/renew via nginx fails at once.
	if out, err := runCmd(cmdDetectTO, "/root", "nginx", "-T"); err == nil &&
		len(nginxFilesServing(out, domain, httpPort)) > 0 {
		return nil
	}
	confPath := filepath.Join(nginxConfDir, "s-ui-acme-"+domain+".conf")
	// 域名有 AAAA 记录时 Let's Encrypt 优先走 IPv6:若本块只听 v4,校验请求会被别的
	// [::]:80 块(如发行版默认站点的 default_server)接走导致 404,故 v6 可用时一并监听。
	listen := "    listen 80;\n"
	if ipv6Available() {
		listen += "    listen [::]:80;\n"
	}
	content := "# Generated by s-ui: ACME HTTP-01 validation block for " + domain + ".\n" +
		"# Kept in place so automatic certificate renewal keeps working.\n" +
		"server {\n" +
		listen +
		"    server_name " + domain + ";\n" +
		"    root /var/www/html;\n" +
		"}\n"
	// 先确认目录本身在:Alpine 用的是 /etc/nginx/http.d,源码编译的 nginx 可能压根没有
	// conf.d。不预判就会撞上一条没有指向性的 ENOENT,与下面「文件写了但没被 include」
	// 是同一类问题(本机 nginx 布局不符合假设),给同样指向根因的错误。
	if st, err := os.Stat(nginxConfDir); err != nil || !st.IsDir() {
		return common.NewErrorf("nginx 配置目录 %s 不存在,无法自动生成验证配置"+
			"(Alpine 通常是 /etc/nginx/http.d,源码编译的 nginx 可能没有此目录);"+
			"请改用 standalone 验证,或手动为 %s 添加一个 server_name 匹配的 server 块",
			nginxConfDir, domain)
	}
	if err := os.WriteFile(confPath, []byte(content), 0644); err != nil {
		return common.NewErrorf("写入 nginx 验证配置失败 %s: %v", confPath, err)
	}
	testOut, err := runCmd(cmdDetectTO, "/root", "nginx", "-t")
	if err != nil {
		_ = os.Remove(confPath) // 绝不留下会卡住 reload 的配置
		return common.NewErrorf("自动生成的 nginx 验证配置未通过 nginx -t,已回滚删除 %s:\n%s", confPath, testOut)
	}
	// The guard above concluded there is no :80 block for this domain. nginx now
	// saying otherwise means the guard missed one that does exist: nginxFilesServing
	// wants listen and server_name in the same server block body, which does not hold
	// when server_name sits in an included snippet (nginx -T dumps every file as its
	// own section) or when its value wraps onto the next line.
	// A warn must not be shrugged off here. On a duplicate nginx keeps whichever it
	// read first, conf.d is read before sites-enabled and s-ui-acme- sorts early
	// within it, so the block that gets dropped is very likely the user's own :80
	// site — replaced by ours, which serves nothing but root /var/www/html.
	if conflictsOnPort(parseNginxConflicts(testOut), domain, httpPort) {
		_ = os.Remove(confPath)
		return common.NewErrorf("nginx 报 %s 在 :80 上 server_name 重名,说明它其实已经有这个域名的 80 端口"+
			"server 块了,只是写法我们认不出来(server_name 写在 include 进来的片段里,或者值换行了)。"+
			"两份同名块会让 nginx 只保留先读到的那份、顶掉另一个——被顶掉的多半是你原有的站点,"+
			"而自动生成的这份只有一个 root。已回滚删除 %s。请确认原有的那个 server 块能被 acme.sh 找到,"+
			"或改用 standalone 验证。\nnginx -t 原文:\n%s", domain, confPath, testOut)
	}
	if out, err := runCmd(cmdDetectTO, "/root", "systemctl", "reload", "nginx"); err != nil {
		_ = os.Remove(confPath)
		return common.NewErrorf("reload nginx 失败,已回滚删除 %s:\n%s", confPath, out)
	}
	// 复验块是否真的生效:若本机 nginx.conf 没有 include conf.d/*.conf(源码编译、
	// openresty、手写配置都常见),上面三步会全部「成功」——文件根本没被解析,nginx -t
	// 自然通过——随后 acme.sh 才报它自己的 "Can not find conf file",离真因十万八千里。
	// 复验把它变成一条指向根因的错误。
	// Same port-aware check as the guard above; without it the 443-only proxy block
	// would fool this into thinking the validation block took effect.
	if out, err := runCmd(cmdDetectTO, "/root", "nginx", "-T"); err != nil ||
		len(nginxFilesServing(out, domain, httpPort)) == 0 {
		_ = os.Remove(confPath)
		// 若文件其实已被 include(nginx -T 因别的原因失败),上面那次 reload 已把块读进
		// 内存,光删文件不足以复原;再 reload 一次抹掉。没被 include 时这次是空操作。
		_, _ = runCmd(cmdDetectTO, "/root", "systemctl", "reload", "nginx")
		return common.NewErrorf("已写入 %s 但它未出现在 nginx 生效配置中,"+
			"本机 nginx.conf 可能没有 include %s/*.conf;已回滚删除,请改用 standalone 验证",
			confPath, nginxConfDir)
	}
	logger.Info("已生成 nginx 验证配置:", confPath)
	return nil
}

// ===== 反向代理 vhost:由面板自动生成、自动校验、失败自动回滚 =====

// Proxy blocks always listen on 443; 80 belongs to the ACME HTTP-01 validation
// block alone. Deciding whether a conflicting-server-name warning means "our
// block got dropped" has to take the port into account — matching on the domain
// alone misreads a duplicate on :80 as a 443 conflict.
const (
	httpPort  = 80
	httpsPort = 443
)

// 生成的文件是 s-ui-proxy-<域名>.conf。前缀刻意和 ACME 验证块的 s-ui-acme- 区分开:
// 清理不再需要的配置时要能 glob 出「我们生成的反代」,又绝不能扫到验证块——
// 那个删掉证书就续不了期了。
//
// First entry is the current prefix (proxyConfPath builds from it); the rest are
// historical. This name changed once (s-ui-panel- -> s-ui-proxy-) and upgraded
// installs still have the old file on disk: it carries the exact same server_name
// on 443 as the freshly generated one, so nginx reports a conflict and drops
// whichever it reads second, and EnsureVhost then rolls back. Without sweeping
// both prefixes the proxy can NEVER be switched on, and every save repeats it.
var nginxProxyConfPrefixes = []string{"s-ui-proxy-", "s-ui-panel-"}

// ProxyEndpoint 是一个域名下的一条反代规则:把 Path 交给本机的某个端口。
// 面板和订阅各算一条;两者共用域名时会落进同一个 server 块的两个 location——
// 各生成一份 server 块会被 nginx 判 conflicting server name 而忽略掉后一个。
type ProxyEndpoint struct {
	Name   string // 只用于配置里的注释和日志:panel / sub
	Path   string // 形如 /app/
	Listen string // 上游监听地址,空表示 0.0.0.0
	Port   int
}

// VhostOptions 描述一个域名要生成的整份配置。
type VhostOptions struct {
	Domain    string
	CertFile  string // 设置里手填的证书,仅当 /root/cert/<域名>/ 下那份不存在时才用
	KeyFile   string
	Endpoints []ProxyEndpoint
}

// ProxySide is the reverse-proxy input for one side (panel or subscription).
// It comes from two places: the form when settings are saved (values not yet in
// the DB), and the database when the panel reconciles at startup. Both paths must
// aggregate to identical results, hence the shared BuildVhostSpecs.
type ProxySide struct {
	Name     string // goes into the config comment and the log: panel / subscription
	Enabled  bool
	Domain   string
	Path     string
	Listen   string
	Port     int
	CertFile string
	KeyFile  string
}

// BuildVhostSpecs groups endpoints by DOMAIN rather than by service: panel and
// subscription sharing one domain is a common setup, and emitting a server block
// each makes nginx report a conflicting server name and drop the second one,
// silently taking one of them offline.
// Argument order is location order (panel first); keeping it fixed makes the
// generated content comparable, which is what lets EnsureVhost short-circuit
// instead of reloading nginx for nothing when nothing changed.
//
// A side that is switched ON with no domain is an error, never a silent skip. It
// is the one input that reads like "disabled" but is not: the service already runs
// plaintext because the switch says so, while skipping it shrinks specs — possibly
// to empty — and SyncVhosts then deletes the generated vhosts as no longer wanted.
// The result is plaintext on the inside, nobody answering on 443, and a save that
// reported success.
func BuildVhostSpecs(sides ...ProxySide) ([]VhostOptions, error) {
	var specs []VhostOptions
	byDomain := map[string]int{}
	for _, s := range sides {
		if !s.Enabled {
			continue
		}
		domain := strings.ToLower(strings.TrimSpace(s.Domain))
		if domain == "" {
			return nil, common.NewErrorf("%s 打开了「由反向代理终结 TLS」却没有填域名:"+
				"反向代理需要它作为 server_name。请先填域名,或把这个开关关掉——"+
				"开着而没有配置意味着服务只跑明文 HTTP,而 443 上没有人接。", s.Name)
		}
		ep := ProxyEndpoint{Name: s.Name, Path: s.Path, Listen: s.Listen, Port: s.Port}
		if i, ok := byDomain[domain]; ok {
			specs[i].Endpoints = append(specs[i].Endpoints, ep)
			continue
		}
		byDomain[domain] = len(specs)
		specs = append(specs, VhostOptions{
			Domain:    domain,
			CertFile:  s.CertFile,
			KeyFile:   s.KeyFile,
			Endpoints: []ProxyEndpoint{ep},
		})
	}
	return specs, nil
}

// VhostResult 回给前端展示:生成了哪个文件、每个端点的对外地址是什么。
type VhostResult struct {
	Domain   string   `json:"domain"`
	ConfFile string   `json:"confFile"`
	CertFile string   `json:"certFile"`
	URLs     []string `json:"urls"`
}

func fileReadable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// nginxVersion 解析 `nginx -v` 的版本号(输出形如 "nginx version: nginx/1.22.1")。
// 取不到时返回 0,0,0,调用方据此走最保守的分支。
func nginxVersion() (major, minor, patch int) {
	out, err := runCmd(cmdDetectTO, "/root", "nginx", "-v")
	if err != nil {
		return 0, 0, 0
	}
	i := strings.LastIndex(out, "/")
	if i < 0 {
		return 0, 0, 0
	}
	fields := strings.Fields(out[i+1:])
	if len(fields) == 0 {
		return 0, 0, 0
	}
	nums := strings.SplitN(fields[0], ".", 3)
	get := func(idx int) int {
		if idx >= len(nums) {
			return 0
		}
		n, err := strconv.Atoi(strings.TrimSpace(nums[idx]))
		if err != nil {
			return 0
		}
		return n
	}
	return get(0), get(1), get(2)
}

// nginxEnv 是生成配置时需要的本机 nginx 事实,由 EnsurePanelProxy 探测后传入,
// 好让 buildPanelProxyConf 保持纯函数、能脱离 nginx 直接验证输出。
type nginxEnv struct {
	http2Listen    string // 加在 listen 后面的 http2 参数(老语法)
	http2Directive string // 独立的 http2 指令行(1.25.1+ 新语法)
	ipv6           bool
}

// http2LinesFor 按 nginx 版本给出开启 HTTP/2 的正确写法:
//   - 1.25.1 起 `listen ... http2` 被废弃,改用独立的 `http2 on;`
//   - 更早的版本只认 listen 上的 http2 参数
//
// 版本探测失败(major==0)时两者都不写:HTTP/1.1 对一个管理面板完全够用,
// 而写错语法会让 nginx -t 直接失败——宁可少个特性,也不能让自动生成失败。
func http2LinesFor(major, minor, patch int) (listenSuffix, directive string) {
	if major == 0 {
		return "", ""
	}
	if major > 1 || (major == 1 && (minor > 25 || (minor == 25 && patch >= 1))) {
		return "", "    http2 on;\n"
	}
	return " http2", ""
}

func detectNginxEnv() nginxEnv {
	env := nginxEnv{ipv6: ipv6Available()}
	env.http2Listen, env.http2Directive = http2LinesFor(nginxVersion())
	return env
}

// resolveDomainCert 挑选喂给 nginx 的证书:优先面板自己用 acme.sh 申请的那份
// (/root/cert/{域名}/),它的路径固定、续期时被原地覆盖;没有才回退到手填的路径。
func resolveDomainCert(domain, certFile, keyFile string) (string, string, error) {
	autoCert := filepath.Join(certBaseDir, domain, "fullchain.pem")
	autoKey := filepath.Join(certBaseDir, domain, "privkey.pem")
	if fileReadable(autoCert) && fileReadable(autoKey) {
		return autoCert, autoKey, nil
	}
	if fileReadable(certFile) && fileReadable(keyFile) {
		return certFile, keyFile, nil
	}
	return "", "", common.NewErrorf("找不到 %s 的证书:请先为它申请证书,"+
		"或登记一份已有的证书文件。(已查 %s 与设置里填写的证书路径)", domain, filepath.Dir(autoCert))
}

// upstreamAddr 是 nginx 要回连的面板地址。面板监听 0.0.0.0/空时回连 127.0.0.1;
// 绑了具体地址就必须照着连,否则 nginx 连不上自己面板。
func upstreamAddr(listen string, port int) string {
	host := strings.TrimSpace(listen)
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	} else if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]" // 裸 IPv6 要加方括号才能跟端口拼
	}
	return host + ":" + strconv.Itoa(port)
}

// normalizeProxyPath 把路径规整成前后都有斜杠的形式(location 前缀匹配要靠它)。
func normalizeProxyPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		p = "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p
}

// buildVhostConf 拼出一个域名的整份配置:一个 server 块,每个端点一个 location。
// 纯函数(本机事实全从 env 传入),好在没有 nginx 的机器上也能验证输出。
func buildVhostConf(opt VhostOptions, certFile, keyFile string, env nginxEnv) string {
	listen := "    listen 443 ssl" + env.http2Listen + ";\n"
	if env.ipv6 {
		listen += "    listen [::]:443 ssl" + env.http2Listen + ";\n"
	}

	var locations strings.Builder
	for _, ep := range opt.Endpoints {
		path := normalizeProxyPath(ep.Path)
		locations.WriteString("\n    # " + ep.Name + "\n")
		// 手输对外地址时几乎一定会漏掉尾斜杠,而 location /app/ 是前缀匹配、配不上 /app。
		// 补一条精确跳转,省掉一个必然会踩的 404。路径就是 / 时无需(也不能)加。
		if trimmed := strings.TrimSuffix(path, "/"); trimmed != "" {
			locations.WriteString("    location = " + trimmed + " { return 301 " + path + "; }\n")
		}
		locations.WriteString("    location " + path + " {\n" +
			"        proxy_pass http://" + upstreamAddr(ep.Listen, ep.Port) + ";\n" +
			"        # 面板/订阅都校验 Host 必须等于设置里的域名,少了这行会对每个请求回 403\n" +
			"        proxy_set_header Host              $host;\n" +
			"        proxy_set_header X-Real-IP         $remote_addr;\n" +
			"        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;\n" +
			"        proxy_set_header X-Forwarded-Proto $scheme;\n" +
			"        proxy_read_timeout 300s;\n" +
			"    }\n")
	}

	return "# Generated by s-ui for " + opt.Domain + " - do not edit.\n" +
		"# The panel rewrites this file whenever the reverse-proxy settings are saved.\n" +
		"server {\n" +
		listen +
		env.http2Directive +
		"    server_name " + opt.Domain + ";\n" +
		"\n" +
		"    ssl_certificate     " + certFile + ";\n" +
		"    ssl_certificate_key " + keyFile + ";\n" +
		"    ssl_protocols       TLSv1.2 TLSv1.3;\n" +
		"    ssl_session_cache   shared:SSL:10m;\n" +
		"    ssl_session_timeout 1d;\n" +
		locations.String() +
		"}\n"
}

// nginx 的语句分隔符:块的 { } 与语句结尾的 ; 一律切开,好让
// `location / { proxy_pass http://127.0.0.1:2095; }` 这种把整块写在一行的紧凑写法
// 也能被按语句解析(只看行首指令会把它读成一条 location 指令,直接漏掉)。
var nginxStmtSplitter = strings.NewReplacer("{", "\n", "}", "\n", ";", "\n")

// nginxProxiesPort 判断 nginx 的生效配置里是否存在指向本机 port 的反向代理。
// 认两种写法:直接 `proxy_pass http://127.0.0.1:PORT`,以及 upstream 块里的
// `server 127.0.0.1:PORT`。用于生成配置后的复验:确认写出去的东西真的被 nginx 读进去了。
func nginxProxiesPort(conf string, port int) bool {
	if port <= 0 {
		return false
	}
	// 注释是到行尾为止的,必须先按行剥掉,再打散成语句
	var sb strings.Builder
	sb.Grow(len(conf))
	for _, line := range strings.Split(conf, "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}

	suffix := ":" + strconv.Itoa(port)
	for _, stmt := range strings.Split(nginxStmtSplitter.Replace(sb.String()), "\n") {
		fields := strings.Fields(stmt)
		// 指令名要精确相等:server_name / proxy_pass_header 这些同前缀指令不能命中,
		// 块开头的 `server {` 切出来只剩一个字段,也在这里被滤掉。
		if len(fields) < 2 || (fields[0] != "proxy_pass" && fields[0] != "server") {
			continue
		}
		for _, f := range fields[1:] {
			// 端口在末尾(host:port),或后面跟着路径(proxy_pass http://host:port/x)。
			// 用 HasSuffix 而不是 Contains,否则 :20950 会被 :2095 命中。
			if strings.HasSuffix(f, suffix) || strings.Contains(f, suffix+"/") {
				return true
			}
		}
	}
	return false
}

// proxyConfPath 是本域名对应的反代配置文件路径。名字里带域名,
// 换域名时生成新文件、旧的由 SyncVhosts 清掉,永远不碰用户自己写的配置。
func proxyConfPath(domain string) string {
	return filepath.Join(nginxConfDir, nginxProxyConfPrefixes[0]+domain+".conf")
}

// vhostDomainOf recovers the domain from a filename WE generated; anything else
// returns "".
//
// It must strip the prefix that actually matched, not the current one: on a file
// with a historical prefix, trimming the current prefix is a no-op and the domain
// comes out as "s-ui-panel-example.com". That never matches SyncVhosts' wanted
// set, so the file slips past the "delete stale copies first" pass and only gets
// removed after generation — which is the exact moment it collides.
// s-ui-acme- is not in the prefix list, so validation blocks always return "" and
// can never be swept away as if they were proxies.
func vhostDomainOf(path string) string {
	base := filepath.Base(path)
	if !strings.HasSuffix(base, ".conf") {
		return ""
	}
	for _, prefix := range nginxProxyConfPrefixes {
		if strings.HasPrefix(base, prefix) {
			return strings.TrimSuffix(strings.TrimPrefix(base, prefix), ".conf")
		}
	}
	return ""
}

// confIsEffective 判断这份文件是否真的被 nginx 读进了生效配置。
// 判据是 nginx -T 自己打的 "# configuration file <路径>:" 行——由 nginx 声明它读了谁,
// 比在输出里找 proxy_pass 可靠:用户自己另有一份反代到同一端口时,只按端口找会把
// 「我们的文件根本没被 include」误判成成功。
func confIsEffective(effectiveConf, confPath string) bool {
	return strings.Contains(effectiveConf, "# configuration file "+confPath+":")
}

// nginxConflict is one parsed "conflicting server name" warning.
type nginxConflict struct {
	Name string // the duplicated server_name
	Addr string // listen address as printed, e.g. 0.0.0.0:443 / [::]:443; empty if unparsed
	Port int    // port taken from Addr; 0 when there is none (unix sockets)
}

// parseNginxConflicts picks every conflicting-server-name warning out of the
// output of nginx -t / -T. nginx prints (src/http/ngx_http.c, ngx_http_server_names):
//
//	nginx: [warn] conflicting server name "example.com" on 0.0.0.0:443, ignored
//
// The address is ngx_sock_ntop's normalised text and varies with listen:
// 0.0.0.0:443, [::]:443, 1.2.3.4:443, [2001:db8::1]:443, and portless unix:/path.
// The port is always whatever follows the last colon, so the bracketed v6 form
// needs no special case. A dual-stack block reports once for v4 and once for v6,
// hence a slice. The warning carries no filename (it goes through ngx_log_error,
// not ngx_conf_log_error), so locating the culprit needs a separate nginx -T.
func parseNginxConflicts(out string) []nginxConflict {
	const marker = "conflicting server name "
	var res []nginxConflict
	for _, line := range strings.Split(out, "\n") {
		i := strings.Index(line, marker)
		if i < 0 {
			continue
		}
		rest := line[i+len(marker):]
		if len(rest) == 0 || rest[0] != '"' {
			continue
		}
		j := strings.IndexByte(rest[1:], '"') // a server_name never contains quotes; first pair wins
		if j < 0 {
			continue
		}
		c := nginxConflict{Name: rest[1 : 1+j]}
		rest = rest[1+j+1:]
		// What is left is ` on <addr>, ignored`. Should the wording ever change we
		// keep just the name and leave Addr empty, letting the caller treat the port
		// as unknown and stay on the safe side.
		const on = " on "
		if strings.HasPrefix(rest, on) {
			addr := rest[len(on):]
			if k := strings.IndexByte(addr, ','); k >= 0 {
				addr = addr[:k]
			}
			c.Addr = strings.TrimSpace(addr)
			if k := strings.LastIndexByte(c.Addr, ':'); k >= 0 {
				if p, err := strconv.Atoi(c.Addr[k+1:]); err == nil {
					c.Port = p
				}
			}
		}
		res = append(res, c)
	}
	return res
}

// conflictsOnPort reports whether any warning means "this domain's block on port
// collided with another one". Case-insensitive: nginx lowercases server_name while
// parsing, and the domain stored in settings is lowercased too.
//
// The port has to be part of the test. Both blocks we generate are single-port —
// the proxy vhost on 443, the ACME validation block on 80 — so a duplicate on the
// other port says nothing about ours, and matching on the domain alone condemns a
// perfectly good config and rolls it back. On the 443 side the rollback deletes the
// file, so on the next save hadPrevious is still false, the identical-content
// short-circuit is unreachable, and every single save replays the same misdiagnosis.
//
// An empty Addr means the wording did not parse and the port is unknown; there we
// would rather over-report than miss one, because both callers treat a hit as
// "roll back and tell the user", which is the safe direction. A parsed address on
// another port (or a portless unix socket) is excluded outright.
func conflictsOnPort(conflicts []nginxConflict, domain string, port int) bool {
	for _, c := range conflicts {
		if strings.EqualFold(c.Name, domain) && (c.Addr == "" || c.Port == port) {
			return true
		}
	}
	return false
}

// nginxConfSection is one section of nginx -T output: a file it read, plus body.
type nginxConfSection struct {
	Path string
	Body string
}

// splitNginxDump cuts the dump on the "# configuration file <path>:" lines nginx
// prints itself. Anything before the first section (warn / ok lines mixed in by
// CombinedOutput) belongs to no file and is dropped.
func splitNginxDump(dump string) []nginxConfSection {
	const header = "# configuration file "
	var out []nginxConfSection
	var sb strings.Builder
	path, open := "", false
	flush := func() {
		if open {
			out = append(out, nginxConfSection{Path: path, Body: sb.String()})
		}
		sb.Reset()
	}
	for _, line := range strings.Split(dump, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, header) && strings.HasSuffix(t, ":") {
			flush()
			path, open = strings.TrimSuffix(strings.TrimPrefix(t, header), ":"), true
			continue
		}
		if open {
			sb.WriteString(line)
			sb.WriteByte('\n')
		}
	}
	flush()
	return out
}

// nginxServerBlocks extracts the body of every server { ... } block (braces
// excluded). It only strips comments and balances braces, without parsing
// directives, so a brace inside a quoted string would cut in the wrong place.
// That is acceptable: this function exists purely to name the offending file in
// an error message, and a bad cut costs at most one filename. Whether to roll
// back does not depend on it at all — vhostConflictsOn443 decides that alone.
func nginxServerBlocks(conf string) []string {
	var sb strings.Builder
	sb.Grow(len(conf))
	for _, line := range strings.Split(conf, "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	s := sb.String()

	var blocks []string
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		// The word right before '{' must be server. An upstream's `server 1.2.3.4:80;`
		// ends in a semicolon with no brace and is excluded for free; a stream server
		// block has no server_name and gets filtered out by the caller.
		head := strings.TrimRight(s[:i], " \t\r\n")
		if head[strings.LastIndexAny(head, " \t\r\n;{}")+1:] != "server" {
			continue
		}
		depth, j := 1, i+1
		for ; j < len(s) && depth > 0; j++ {
			if s[j] == '{' {
				depth++
			} else if s[j] == '}' {
				depth--
			}
		}
		if depth == 0 {
			blocks = append(blocks, s[i+1:j-1])
			i = j - 1
		}
	}
	return blocks
}

// nginxListensOn reports whether a server block body listens on port.
// Understands `listen 443 ssl;`, `listen [::]:443 ssl;`, `listen 1.2.3.4:443;`,
// `listen *:443;`, and the address-only `listen 1.2.3.4 ssl;` (where nginx
// defaults to 443 or 80 depending on the ssl parameter).
func nginxListensOn(block string, port int) bool {
	for _, stmt := range strings.Split(nginxStmtSplitter.Replace(block), "\n") {
		fields := strings.Fields(stmt)
		if len(fields) < 2 || fields[0] != "listen" {
			continue
		}
		p := 0
		if n, err := strconv.Atoi(fields[1]); err == nil {
			p = n
		} else if i := strings.LastIndexByte(fields[1], ':'); i >= 0 {
			p, _ = strconv.Atoi(fields[1][i+1:]) // unix:/path does not parse; stays 0
		} else {
			p = httpPort
			for _, f := range fields[2:] {
				if f == "ssl" {
					p = httpsPort
				}
			}
		}
		if p == port {
			return true
		}
	}
	return false
}

// nginxFilesServing returns which files in the dump provide a server block for
// domain on port, deduplicated and in nginx's own read order.
// Two callers: the conflicting warning carries no filename, and without this the
// user has no idea which file to edit; and ensureNginxServerBlock, which needs to
// know whether the :80 validation block exists — matching server_name alone there
// gets fooled by the 443-only block the reverse proxy generates.
func nginxFilesServing(dump, domain string, port int) []string {
	var out []string
	seen := map[string]bool{}
	for _, sec := range splitNginxDump(dump) {
		if seen[sec.Path] {
			continue
		}
		for _, b := range nginxServerBlocks(sec.Body) {
			// Split into statements first. nginxHasServerName parses line by line, but a
			// block body may well sit entirely on one line
			// (`server { listen 443 ssl; server_name a.com; }`), where server_name is not
			// at the start of a line and would be missed. nginxStmtSplitter is idempotent,
			// so nginxListensOn replacing again internally is harmless.
			stmts := nginxStmtSplitter.Replace(b)
			if nginxListensOn(stmts, port) && nginxHasServerName(stmts, domain) {
				seen[sec.Path] = true
				out = append(out, sec.Path)
				break
			}
		}
	}
	return out
}

// precheckVhost is everything EnsureVhost asks before it touches the disk: are the
// options well formed, is nginx usable, is there a certificate. It normalises
// opt.Domain in place and hands back the resolved certificate paths.
//
// It is separate because saving settings sometimes has to ask the very same
// questions WITHOUT writing anything. Once the proxy is on, this panel's own page
// is the nginx location, so rewriting the vhost before api/save cuts the road that
// request travels on; that path can only validate now and let the startup
// reconciliation (syncNginxProxy in app.Start) do the writing after the restart.
// Validation and generation must ask one identical set of questions — if they
// drift, a save passes and the reconciliation fails afterwards, by which point the
// service already runs plaintext with nothing answering on 443 and nobody left to
// report it.
func (a *AcmeService) precheckVhost(opt *VhostOptions) (certFile, keyFile string, err error) {
	if runtime.GOOS == "windows" {
		return "", "", common.NewError("Windows 不支持自动生成 nginx 配置")
	}
	opt.Domain = strings.TrimSpace(opt.Domain)
	if opt.Domain == "" {
		return "", "", common.NewError("请先填写「域名」:反向代理需要它作为 server_name")
	}
	if !validDomain(opt.Domain) {
		return "", "", common.NewErrorf("域名含非法字符: %q", opt.Domain)
	}
	if len(opt.Endpoints) == 0 {
		return "", "", common.NewErrorf("%s 没有需要反代的服务", opt.Domain)
	}
	for _, ep := range opt.Endpoints {
		if ep.Port <= 0 {
			return "", "", common.NewErrorf("%s 的端口无效: %d", ep.Name, ep.Port)
		}
	}

	status := a.DetectNginx()
	if !status.Installed {
		return "", "", common.NewError("未检测到 nginx,无法自动生成反向代理配置;请先安装 nginx(apt install nginx)后重试")
	}
	if !status.Active {
		return "", "", common.NewError("nginx 已安装但没有运行,无法确认配置是否生效;请先启动它(systemctl start nginx)后重试")
	}
	if st, err := os.Stat(nginxConfDir); err != nil || !st.IsDir() {
		return "", "", common.NewErrorf("nginx 配置目录 %s 不存在,无法自动生成"+
			"(Alpine 通常是 /etc/nginx/http.d,源码编译的 nginx 可能没有此目录)", nginxConfDir)
	}

	return resolveDomainCert(opt.Domain, opt.CertFile, opt.KeyFile)
}

// CheckVhosts answers two questions without writing a single byte or reloading
// nginx:
//
//  1. would generating this set fail right now (a missing certificate being by far
//     the most common reason), returned as an error;
//  2. is nginx already exactly this, returned as drift=false.
//
// Question 1 exists for the save path while the proxy is on: it cannot rewrite the
// vhost before saving (this page is that location), so it saves, restarts, and lets
// the startup reconciliation write. Nobody can report a failure in that gap — the
// service is already plaintext by then — so the failure has to be surfaced before
// the save, and this is the only way to ask without writing.
//
// Question 2 is the self-healing handle. When a reconciliation failed at the last
// restart, or the file was removed by hand, nothing says so: the settings page
// looks fine while the panel is gone from 443. Reporting drift lets the page point
// at the "restart panel" button it already has, which re-runs the reconciliation.
func (a *AcmeService) CheckVhosts(specs []VhostOptions) (drift bool, err error) {
	if len(specs) == 0 {
		return false, nil
	}
	// Read the effective config once for the whole batch. If nginx -T cannot be run
	// we do not guess: the "file is right but was never included" case is dropped and
	// only the on-disk comparison decides. This is a hint on a settings page, and a
	// warning that a failed probe pins there permanently is worse than a missed one.
	var effective string
	if out, e := runCmd(cmdDetectTO, "/root", "nginx", "-T"); e == nil {
		effective = out
	}
	env := detectNginxEnv()
	for _, spec := range specs {
		certFile, keyFile, err := a.precheckVhost(&spec)
		if err != nil {
			return false, err
		}
		// Same yardstick as EnsureVhost's short-circuit: identical content AND nginx
		// really did read the file. Anything else means what is running is not what the
		// settings say.
		confPath := proxyConfPath(spec.Domain)
		previous, readErr := os.ReadFile(confPath)
		if readErr != nil || string(previous) != buildVhostConf(spec, certFile, keyFile, env) ||
			(effective != "" && !confIsEffective(effective, confPath)) {
			drift = true
		}
	}
	return drift, nil
}

// EnsureVhost 为一个域名生成 nginx 反向代理配置并确认它真的生效。
//
// 全过程「验证通过才算成功」:nginx -t 不过、和用户已有配置撞 server_name、reload 失败、
// 或者块压根没进入生效配置,都会删掉文件、把 nginx 恢复原状,再带着 nginx 自己的输出报错。
// 调用方必须在这里成功之后才把面板/订阅切成明文 HTTP —— 顺序反了就会出现
// 「服务不再终结 TLS,前面又没人接管」的窗口,而关掉开关的入口正在那个打不开的页面里。
func (a *AcmeService) EnsureVhost(opt VhostOptions) (*VhostResult, error) {
	certFile, keyFile, err := a.precheckVhost(&opt)
	if err != nil {
		return nil, err
	}

	confPath := proxyConfPath(opt.Domain)
	// 覆盖前留个底,失败时要原样还回去(重复生成是常态:改端口、改路径都会走到这里)
	previous, readErr := os.ReadFile(confPath)
	hadPrevious := readErr == nil
	restore := func() {
		if hadPrevious {
			_ = os.WriteFile(confPath, previous, 0644)
		} else {
			_ = os.Remove(confPath)
		}
		// 上面那次 reload 可能已经把坏配置读进内存了,光改文件不够,必须再 reload 一次
		_, _ = runCmd(cmdDetectTO, "/root", "systemctl", "reload", "nginx")
	}

	result := &VhostResult{Domain: opt.Domain, ConfFile: confPath, CertFile: certFile}
	for _, ep := range opt.Endpoints {
		result.URLs = append(result.URLs, "https://"+opt.Domain+normalizeProxyPath(ep.Path))
	}
	// 复验用第一个端点的端口即可:整份配置是一起写进去的,一个 location 在、其余就在
	probePort := opt.Endpoints[0].Port

	newContent := buildVhostConf(opt, certFile, keyFile, detectNginxEnv())
	// 调用方每次保存设置都会走到这里(这样「开关开着但配置丢了」的老实例也能自愈)。
	// 内容一模一样、而且确实已经在生效配置里,就什么都不用做——否则改个时区都要 reload 一次 nginx。
	if hadPrevious && string(previous) == newContent {
		if effective, err := runCmd(cmdDetectTO, "/root", "nginx", "-T"); err == nil &&
			confIsEffective(effective, confPath) && nginxProxiesPort(effective, probePort) {
			return result, nil
		}
	}

	if err := os.WriteFile(confPath, []byte(newContent), 0644); err != nil {
		return nil, common.NewErrorf("写入 nginx 配置失败 %s: %v", confPath, err)
	}

	out, err := runCmd(cmdDetectTO, "/root", "nginx", "-t")
	if err != nil {
		restore()
		return nil, common.NewErrorf("生成的 nginx 配置未通过 nginx -t,已回滚:\n%s", out)
	}
	// With two server blocks for one domain on 443, nginx keeps whichever it parsed
	// first, drops the other, emits a single warn, and -t still passes. Skipping
	// this check would "succeed" meaninglessly: file present, nginx healthy, panel
	// unreachable. Which one loses depends on include order (Debian reads conf.d
	// before sites-enabled, and globs conf.d alphabetically), so the block that gets
	// dropped may be the user's site or may be ours. Both are broken states, so roll
	// back either way and let the user decide which copy to keep.
	//
	// This check is the ONLY line of defence: nginx -T dumps during parsing, before
	// the conflict is adjudicated, so a dropped block is still dumped. That makes
	// the confIsEffective + nginxProxiesPort pair below always true here — they
	// cannot detect a block that got ignored.
	conflicts := parseNginxConflicts(out)
	if conflictsOnPort(conflicts, opt.Domain, httpsPort) {
		// Dump once more BEFORE rolling back (restore deletes our file) to list the
		// other files serving a 443 block — the warning itself carries no filename, and
		// without this the user cannot tell which one to edit. Failing to locate it
		// does not change the verdict, it only makes the message weaker.
		where := "没能定位到冲突的文件,请自行查 nginx -T 的输出"
		if dump, e := runCmd(cmdDetectTO, "/root", "nginx", "-T"); e == nil {
			var others []string
			files := nginxFilesServing(dump, opt.Domain, httpsPort)
			for _, f := range files {
				if f != confPath {
					others = append(others, f)
				}
			}
			switch {
			case len(others) > 0:
				where = "冲突的另一份在:" + strings.Join(others, "、")
			case len(files) > 0:
				where = "只有 " + confPath + " 提供 443 块,多半是 nginx.conf 把它 include 了两次"
			}
		}
		restore()
		return nil, common.NewErrorf("nginx 里有不止一份 %s 的 443 配置,它只保留先读到的那份、"+
			"忽略其余的(被顶掉的可能是你原有的站点,也可能是这份自动生成的);已回滚。%s。"+
			"请只保留一份:在你要留下的那个 server 块里把 %s 反代到 http://%s,"+
			"并加上 proxy_set_header Host $host;\nnginx -t 原文:\n%s",
			opt.Domain, where, normalizeProxyPath(opt.Endpoints[0].Path),
			upstreamAddr(opt.Endpoints[0].Listen, opt.Endpoints[0].Port), out)
	}
	// Any remaining duplicate is not on 443 (typically :80). It does not affect this
	// vhost and must not block the save, but it can make acme.sh --nginx pick the
	// wrong block during validation, so leave a log line for later.
	for _, c := range conflicts {
		if strings.EqualFold(c.Name, opt.Domain) {
			logger.Warning("nginx 里有 ", opt.Domain, " 的重名 server 块(", c.Addr,
				"),不影响本次反代,但可能影响 acme.sh 的 HTTP-01 验证")
		}
	}
	if out, err := runCmd(cmdDetectTO, "/root", "systemctl", "reload", "nginx"); err != nil {
		restore()
		return nil, common.NewErrorf("reload nginx 失败(443 端口可能被别的程序占用),已回滚:\n%s", out)
	}
	// 复验:若本机 nginx.conf 没有 include conf.d/*.conf(源码编译、openresty、手写配置
	// 都常见),上面每一步都会「成功」,而文件根本没被解析。这一步把它变成明确的失败。
	// 两个判据都要:文件确实被读了(confIsEffective),且里面的反代端口确实是面板的。
	if effective, err := runCmd(cmdDetectTO, "/root", "nginx", "-T"); err != nil ||
		!confIsEffective(effective, confPath) || !nginxProxiesPort(effective, probePort) {
		restore()
		return nil, common.NewErrorf("已写入 %s,但它没有出现在 nginx 的生效配置里,"+
			"本机 nginx.conf 可能没有 include %s/*.conf;已回滚",
			confPath, nginxConfDir)
	}

	logger.Info("已生成反向代理配置:", confPath)
	return result, nil
}

// SyncVhosts 让 nginx 里「我们生成的那些反代配置」与传入的期望状态一致:
// specs 里的每个域名各生成一份,而以前生成过、现在不在 specs 里的一律删掉。
//
// 删除这一步是必须的,漏了会留下两种坏状态:关掉反代后 443 还在把明文请求转给一个
// 已经改回 TLS 的端口(502),换域名后旧域名的块还赖在 nginx 里。
// 清理只扫 s-ui-proxy-* 前缀,碰不到 ACME 验证块,更碰不到用户自己写的配置。
func (a *AcmeService) SyncVhosts(specs []VhostOptions) ([]*VhostResult, error) {
	if runtime.GOOS == "windows" {
		if len(specs) == 0 {
			return nil, nil
		}
		return nil, common.NewError("Windows 不支持自动生成 nginx 配置")
	}

	wanted := make(map[string]bool, len(specs))
	for _, s := range specs {
		wanted[strings.ToLower(strings.TrimSpace(s.Domain))] = true
	}

	// 先生成,全部成功后再删多余的。反过来的话,生成失败(典型:换成的新域名还没有
	// 证书)时旧配置已经被删,而调用方失败时是【不保存设置】的——磁盘上的反代被销毁、
	// 数据库里还是旧配置,正在服务的域名就此断掉,且要等到下一次 reload 才暴露。
	// 同域名的更新由 EnsureVhost 原地覆盖 + 失败回滚,不依赖先删。
	//
	// EnsureVhost 只回滚它自己那个域名;而批次里排在前面的域名一旦成功就已经写盘 +
	// reload。后面任一域名失败时,调用方是【不保存设置】的,那些先生效的 vhost 就成了
	// 「库里没记、nginx 却在转发」的孤儿(典型:面板有证书、订阅没有,面板那份先生效,
	// save 却因订阅失败整体中止,443 上多出一份没人管的反代)。所以逐个记下前一状态,
	// 失败时把这一批已应用的一起还原,让磁盘和「未保存」的语义对齐。
	// The "delete stale copies first" pass below records into this too: what it
	// removes may well be a config that is currently serving traffic.
	type appliedVhost struct {
		path        string
		prev        []byte
		hadPrevious bool
	}
	var applied []appliedVhost
	rollbackApplied := func() {
		if len(applied) == 0 {
			return
		}
		for _, v := range applied {
			if v.hadPrevious {
				_ = os.WriteFile(v.path, v.prev, 0644)
			} else {
				_ = os.Remove(v.path)
			}
		}
		if out, err := runCmd(cmdDetectTO, "/root", "systemctl", "reload", "nginx"); err != nil {
			logger.Warning("回滚本批反代配置后 reload nginx 失败(重启 nginx 即可生效):", out)
		}
	}

	// Renamed leftovers must go first: if a domain still has an old conf at a
	// different path (prefix changed, or case differs), nginx calls the two a
	// conflicting server name and silently drops the one it reads second — conf.d is
	// globbed alphabetically and s-ui-panel- sorts before s-ui-proxy-, so the one
	// dropped is precisely the newly generated file. This domain is about to be
	// regenerated, so delete now; leaving it until after generation is already too late.
	//
	// Deleting first is not free when generation then fails: the pre-rename copy is
	// very likely serving traffic (every upgraded install is in that state), and on
	// failure the caller does NOT save the settings. So take a copy first and record
	// it in applied, letting rollbackApplied restore it — one failed save must not
	// take a live 443 entrypoint with it.
	removed := 0
	list := func() []string {
		var entries []string
		for _, prefix := range nginxProxyConfPrefixes {
			e, _ := filepath.Glob(filepath.Join(nginxConfDir, prefix+"*.conf"))
			entries = append(entries, e...)
		}
		return entries
	}
	for _, p := range list() {
		domain := vhostDomainOf(p)
		if domain == "" {
			continue
		}
		if wanted[strings.ToLower(domain)] && p != proxyConfPath(strings.ToLower(domain)) {
			// 读不出来就别删。回滚全靠这份副本,没有它这次删除就是不可逆的,而这个文件
			// 很可能正在 443 上服务。留着它最坏是撞一次 server_name 冲突、被 EnsureVhost
			// 拦下并报错,那是可恢复的;删掉找不回来才不是。
			prev, err := os.ReadFile(p)
			if err != nil {
				logger.Warning("跳过删除路径不一致的旧反代配置(读取失败,删了就无法回滚) ", p, ": ", err)
				continue
			}
			if err := os.Remove(p); err == nil {
				logger.Info("已删除路径不一致的旧反代配置:", p)
				removed++
				applied = append(applied, appliedVhost{path: p, prev: prev, hadPrevious: true})
			}
		}
	}

	var results []*VhostResult
	for _, spec := range specs {
		// 路径口径与 EnsureVhost 内部一致(只 TrimSpace,不额外 ToLower)
		confPath := proxyConfPath(strings.TrimSpace(spec.Domain))
		prev, readErr := os.ReadFile(confPath)
		res, err := a.EnsureVhost(spec)
		if err != nil {
			// 失败的这个域名 EnsureVhost 已自行回滚;这里还原它之前【已成功】的那些
			rollbackApplied()
			return nil, err
		}
		applied = append(applied, appliedVhost{path: confPath, prev: prev, hadPrevious: readErr == nil})
		results = append(results, res)
	}

	for _, p := range list() {
		domain := vhostDomainOf(p)
		if domain == "" || wanted[strings.ToLower(domain)] {
			continue
		}
		if err := os.Remove(p); err != nil {
			logger.Warning("删除失效的反代配置失败 ", p, ": ", err)
			continue
		}
		logger.Info("已删除不再需要的反向代理配置:", p)
		removed++
	}
	// 有删除就必须 reload,不能指望上面的 EnsureVhost 顺带做:内容没变时它会短路返回、
	// 根本不 reload,被删掉的 server 块就一直留在 nginx 内存里继续转发。
	if removed > 0 {
		if out, err := runCmd(cmdDetectTO, "/root", "systemctl", "reload", "nginx"); err != nil {
			logger.Warning("已删除反代配置,但 reload nginx 失败(重启 nginx 即可生效):", out)
		}
	}
	return results, nil
}

// ensureAcmeSh 确保 acme.sh 可用,返回其可执行路径与对应 HOME;缺失时自动安装。
// 安装时在 shell 内显式 export HOME=/root,确保装到 /root/.acme.sh(systemd 下
// HOME 常为空,否则会装到 /.acme.sh);并 curl/wget 自适应(最小化系统可能只有其一)。
func ensureAcmeSh() (bin, home string, err error) {
	if bin, home = resolveAcmeSh(); bin != "" {
		return bin, home, nil
	}
	logger.Info("acme.sh 未安装,开始自动安装...")
	installScript := "export HOME=/root; " +
		"if command -v curl >/dev/null 2>&1; then curl https://get.acme.sh | sh; " +
		"else wget -O - https://get.acme.sh | sh; fi"
	out, e := runCmd(acmeInstallTO, "/root", "sh", "-c", installScript)
	if e != nil {
		return "", "", common.NewErrorf("自动安装 acme.sh 失败,请在服务器手动安装(s-ui 脚本 SSL 菜单)。详情:\n%s", out)
	}
	if bin, home = resolveAcmeSh(); bin != "" {
		logger.Info("acme.sh 安装成功:", bin)
		return bin, home, nil
	}
	return "", "", common.NewErrorf("acme.sh 安装后仍未找到(已查 $HOME/.acme.sh、/root/.acme.sh、/.acme.sh)。安装输出:\n%s", out)
}

// ensureSocat 仅 standalone 申请需要,best-effort 安装,失败不致命(由后续申请报真实错)。
func ensureSocat() {
	if _, ok := resolveBin("socat"); ok {
		return
	}
	logger.Info("socat 未安装,尝试自动安装(standalone 申请需要)...")
	managers := [][]string{
		{"apt", "-y", "install", "socat"},
		{"yum", "-y", "install", "socat"},
		{"dnf", "-y", "install", "socat"},
		{"pacman", "-Sy", "--noconfirm", "socat"},
	}
	for _, m := range managers {
		if _, ok := resolveBin(m[0]); ok {
			if _, err := runCmd(acmeInstallTO, "/root", m[0], m[1:]...); err == nil {
				return
			}
		}
	}
	logger.Warning("socat 自动安装失败,若 standalone 申请报错请手动安装 socat")
}

// IssueWeb 为面板/订阅申请证书并安装到 /root/cert/{域名}/。
//   - method     :standalone / nginx / auto(空视同 auto),解析与可行性校验见 resolveMethod。
//   - force      :域名已有未到期证书时 acme.sh 默认跳过签发,force 时加 --force 强制续期。
//   - behindProxy:webNginx=true,即反向代理终结 TLS、nginx 是证书消费方(决定 reloadcmd)。
func (a *AcmeService) IssueWeb(domain, email, method string, force, behindProxy bool) (*IssueResult, error) {
	if runtime.GOOS == "windows" {
		return nil, common.NewError("Windows 不支持 acme.sh 申请证书")
	}
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return nil, common.NewError("域名不能为空")
	}
	if !validDomain(domain) {
		return nil, common.NewErrorf("域名含非法字符: %q", domain)
	}
	// 同一时刻只允许一个申请,避免重复点击并发申请撞 Let's Encrypt 限速
	if !acmeIssuing.TryLock() {
		return nil, common.NewError("已有证书申请正在进行,请等当前申请完成后再试")
	}
	defer acmeIssuing.Unlock()

	resolved, err := a.resolveMethod(method)
	if err != nil {
		return nil, err
	}

	bin, home, err := ensureAcmeSh()
	if err != nil {
		return nil, err
	}

	// 默认 CA 设为 Let's Encrypt(与脚本一致)
	if out, err := runCmd(cmdDetectTO, home, bin, "--set-default-ca", "--server", "letsencrypt"); err != nil {
		return nil, common.NewErrorf("设置默认 CA 失败:\n%s", out)
	}

	// 申请证书
	issueArgs := []string{"--issue", "-d", domain}
	if email != "" {
		issueArgs = append(issueArgs, "--accountemail", email)
	}
	if resolved == methodNginx {
		if err := a.ensureNginxServerBlock(domain); err != nil {
			return nil, err
		}
		issueArgs = append(issueArgs, "--nginx")
	} else {
		ensureSocat()
		// resolveMethod 的 80 端口预检发生在 ensureAcmeSh / ensureSocat 之前,而这两步
		// 首次运行各可能耗掉 120s 装包——恰恰是包管理器可能拉起 :80 上某个服务的时刻。
		// 预检负责给出清晰的早期错误,这里紧贴使用点再确认一次,收窄那段窗口。
		if !port80Free() {
			return nil, common.NewError("80 端口在准备阶段被占用,无法用 standalone 申请;请停止占用 80 端口的程序后重试")
		}
		issueArgs = append(issueArgs, "--standalone", "--httpport", "80")
		// 纯 IPv6 主机必须显式切到 v6 监听,否则 acme.sh 只绑 v4、LE 的请求根本进不来。
		// 判据是「有没有全局 v4」而非「内核支不支持 v6」——该标志是排他的,见 hasGlobalIPv4。
		if !hasGlobalIPv4() {
			issueArgs = append(issueArgs, "--listen-v6")
		}
	}
	// 域名已有未到期证书时 acme.sh 会跳过("Skipping. Next renewal time is ...")，
	// --force 强制重新签发以续期。会消耗 Let's Encrypt 限速额度，故由前端「强制续期」显式触发。
	if force {
		issueArgs = append(issueArgs, "--force")
	}
	if out, err := runCmd(acmeIssueTO, home, bin, issueArgs...); err != nil {
		hint := "证书申请失败"
		if !force {
			hint = "证书申请失败(若域名已有未到期证书,请改用「强制续期」)"
		}
		return nil, common.NewErrorf("%s:\n%s", hint, out)
	}

	// 安装证书到 /root/cert/{域名}/
	certDir := filepath.Join(certBaseDir, domain)
	if err := os.MkdirAll(certDir, 0755); err != nil {
		return nil, common.NewErrorf("创建证书目录失败 %s: %v", certDir, err)
	}
	keyFile := filepath.Join(certDir, "privkey.pem")
	certFile := filepath.Join(certDir, "fullchain.pem")
	installArgs := []string{
		"--installcert", "-d", domain,
		"--key-file", keyFile,
		"--fullchain-file", certFile,
	}
	// reloadcmd 按「谁消费证书」决定(见 buildReloadCmd)。standalone 且面板自用时不配:
	//  - acme.sh 首次 --installcert 会内联执行一次 reloadcmd,若是重启面板会杀掉正在
	//    处理本次申请请求的进程,前端拿不到结果(即便证书已申请成功);
	//  - 面板/订阅侧也无需 reloadcmd:network/tls.go 的 certReloader 每次 TLS 握手按
	//    文件 mtime 热加载,续期覆盖 /root/cert 下的文件后自动生效。
	reloadCmd := a.buildReloadCmd(resolved, behindProxy)
	if reloadCmd != "" {
		installArgs = append(installArgs, "--reloadcmd", reloadCmd)
	}
	if out, err := runCmd(acmeIssueTO, home, bin, installArgs...); err != nil {
		return nil, common.NewErrorf("安装证书失败:\n%s", out)
	}

	// 启用 acme.sh 自带 cron 自动续期(失败不影响本次证书)
	if _, err := runCmd(acmeInstallTO, home, bin, "--upgrade", "--auto-upgrade"); err != nil {
		logger.Warning("启用 acme.sh 自动续期失败(不影响本次证书):", err)
	}

	return &IssueResult{CertFile: certFile, KeyFile: keyFile, Method: resolved, ReloadCmd: reloadCmd}, nil
}

// buildReloadCmd 决定续期成功后的重载命令,按「谁消费证书」而非验证方式:
//   - nginx 验证或反代终结 TLS(behindProxy):nginx 持有/使用证书,续期后必须让它
//     重读,否则续期只覆盖了磁盘文件,nginx 仍用内存里的旧证书,90 天后线上过期。
//     用 try-reload-or-restart:nginx 在跑→reload;没在跑→无事退 0,避免 reloadcmd
//     失败把 --installcert 整体判失败(彼时证书文件其实已安装成功)。
//   - 其余(standalone 且面板/订阅自用):返回空——证书由 certReloader 热加载,
//     无需外部命令(见 IssueWeb 注释)。
//
// behindProxy 只说明「TLS 由反向代理终结」,没说那代理是 nginx——Caddy / Traefik /
// HAProxy 的用户同样会开这个开关。此时不能硬发 nginx 命令:try-reload-or-restart
// 的「不在跑就空转退 0」只对【已知但未激活】的 unit 成立,unit 根本不存在时 systemd
// 以 5 退出,会让 --installcert 整体判失败(彼时证书其实已落盘),且这条注定失败的
// 命令还会被写进 acme.sh 的域名 conf,让此后每次续期都报错。systemctl 本身缺席
// (Alpine/OpenRC、Devuan、容器里直跑 nginx)以 127 退出,后果完全一样。
// 这条路径经 behindProxy 可达(代理只听 443 → 80 空闲 → standalone 验证 → 走到
// 这里),不是理论情况;确认不了就留空,由调用方提示用户自行配置重载钩子。
func (a *AcmeService) buildReloadCmd(method string, behindProxy bool) string {
	if method != methodNginx && !behindProxy {
		return ""
	}
	// 判据必须是「systemd 认识 nginx 这个 unit」,不能是「nginx 二进制在」:源码编译的
	// nginx 装在 /usr/local/sbin(正好在 fallbackPath 上)却往往没有 unit 文件,查
	// 二进制会放行,随后 try-reload-or-restart 退 5,正是上面要避免的那种失败。
	// systemctl cat 恰好是需要的语义:unit 已知→退 0(哪怕没在跑),不存在→非 0;
	// systemctl 自身缺席时 runCmd 直接报 not found,同样落进 err 分支。
	if _, err := runCmd(cmdDetectTO, "/root", "systemctl", "cat", "nginx"); err != nil {
		return ""
	}
	return "systemctl try-reload-or-restart nginx"
}

// ===== 证书清单 =====

// CertInfo 是「域名与证书」页面上的一条记录。
// 时间统一用 unix 秒回给前端,由前端按浏览器时区渲染并算剩余天数——服务器时区不一定
// 是用户的,后端算好天数反而会差一天。
type CertInfo struct {
	Domain    string `json:"domain"`
	CertFile  string `json:"certFile"`  // 摆出来供复制:建入站 TLS 时要填进 certificate_path
	KeyFile   string `json:"keyFile"`
	CA        string `json:"ca"`
	KeyType   string `json:"keyType"`
	NotAfter  int64  `json:"notAfter"`  // 0 表示证书文件读不到
	NextRenew int64  `json:"nextRenew"` // 0 表示不适用(手动登记的证书)
	Managed   bool   `json:"managed"`   // true = acme.sh 维护并自动续期
}

// parseAcmeConf 解析 acme.sh 的域名 conf(纯 KEY='VALUE' 行)。
// 不用解析 `acme.sh --list` 的表格输出:那是空格对齐的,Profile 一列为空时字段会整体
// 错位,拿 CA 当创建时间。这个文件是 acme.sh 自己 source 的,格式稳定得多。
func parseAcmeConf(content string) map[string]string {
	kv := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		eq := strings.IndexByte(line, '=')
		if eq <= 0 || strings.HasPrefix(line, "#") {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		val = strings.TrimPrefix(val, "'")
		val = strings.TrimSuffix(val, "'")
		kv[key] = val
	}
	return kv
}

// readLeafCert 解析证书文件的第一段 PEM,也就是叶子证书。fullchain 里后面跟着的是
// 中间 CA,它的有效期比叶子长得多、Issuer 也是上一级,拿错了会把「快过期」显示成
// 「还早」。读不到或解析失败一律返回 nil,由调用方降级显示。
func readLeafCert(path string) *x509.Certificate {
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil
	}
	return cert
}

// certNotAfter 读证书文件的到期时间,0 表示读不到。
func certNotAfter(path string) int64 {
	if cert := readLeafCert(path); cert != nil {
		return cert.NotAfter.Unix()
	}
	return 0
}

// certIssuer 读签发者名字,给手动登记的证书当 CA 显示。acme.sh 那边不用它:
// 有 Le_API 可查,比 Issuer CN 准(同一家 CA 的 CN 会随中间证书轮换而变)。
// 收解析好的叶子证书而不是路径:调用方本来就要解析一次,别为两个字段读两遍文件。
func certIssuer(cert *x509.Certificate) string {
	if cert == nil {
		return ""
	}
	if cn := strings.TrimSpace(cert.Issuer.CommonName); cn != "" {
		return cn
	}
	if len(cert.Issuer.Organization) > 0 {
		return strings.TrimSpace(cert.Issuer.Organization[0])
	}
	return ""
}

// caName 把 acme.sh 记的 CA 目录 URL 变成人看的名字。
func caName(apiURL string) string {
	switch {
	case apiURL == "":
		return ""
	case strings.Contains(apiURL, "letsencrypt"):
		return "Let's Encrypt"
	case strings.Contains(apiURL, "zerossl"):
		return "ZeroSSL"
	case strings.Contains(apiURL, "buypass"):
		return "Buypass"
	// Google 的 ACME 端点是 dv.acme-v02.api.pki.goog,串里没有 "google" 这个词
	case strings.Contains(apiURL, "pki.goog"):
		return "Google Trust Services"
	case strings.Contains(apiURL, "ssl.com"):
		return "SSL.com"
	}
	if u, err := url.Parse(apiURL); err == nil && u.Host != "" {
		return u.Host
	}
	return apiURL
}

// ListCerts 列出 acme.sh 维护的证书。直接读它的家目录,不调 `acme.sh --list`。
// 找不到 acme.sh 时返回空列表而不是错误:那只是还没申请过证书,页面该显示空态。
func (a *AcmeService) ListCerts() ([]CertInfo, error) {
	if runtime.GOOS == "windows" {
		return []CertInfo{}, nil
	}
	bin, _ := resolveAcmeSh()
	if bin == "" {
		return []CertInfo{}, nil
	}
	// 注意别用 resolveAcmeSh 返回的第二个值:那是给子进程当 HOME 用的上级目录(/root),
	// 证书目录在 acme.sh 自己的家目录里(/root/.acme.sh),也就是可执行文件所在目录。
	dataDir := filepath.Dir(bin)
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return []CertInfo{}, nil
	}

	// 同一个域名可能有两个目录(<域名> 与 <域名>_ecc:先手工签过 RSA、再由面板签
	// ec-256 就会这样),必须按域名去重。两条同名记录会让前端 v-for 撞 key,而
	// sort.Slice 对相等键不稳定,findCert 取到哪条随每次刷新变——webCertFile 会在
	// 两个路径之间来回改写。留 NotAfter 较新的那份:那才是还有人在续期的。
	type candidate struct {
		info CertInfo
		ecc  bool
	}
	byDomain := map[string]candidate{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// 证书目录名是 <域名> 或 <域名>_ecc;ca/deploy/dnsapi/notify 这些内部目录
		// 里没有同名的 .conf,下面读文件时自然被滤掉,不用维护一份黑名单。
		domain := strings.TrimSuffix(e.Name(), "_ecc")
		raw, err := os.ReadFile(filepath.Join(dataDir, e.Name(), domain+".conf"))
		if err != nil {
			continue
		}
		kv := parseAcmeConf(string(raw))
		if kv["Le_Domain"] != "" {
			domain = kv["Le_Domain"]
		}

		info := CertInfo{
			// 对外统一小写:DNS 不分大小写,而下游(这里的去重、前端 findCert、删除
			// 守卫)全是精确比对,大小写混着会让同一个域名分裂成两条真相。
			// 文件路径仍用目录里的原样写法,大小写敏感的文件系统上不能改。
			Domain:  strings.ToLower(domain),
			CA:      caName(kv["Le_API"]),
			KeyType: kv["Le_Keylength"],
			Managed: true,
		}
		// 优先用 acme.sh 记录的安装路径:那才是面板/nginx/sing-box 实际在读的文件。
		// 没装过就退回 acme.sh 自己的副本,至少能显示到期时间。
		info.CertFile = kv["Le_RealFullChainPath"]
		info.KeyFile = kv["Le_RealKeyPath"]
		if info.CertFile == "" {
			info.CertFile = filepath.Join(dataDir, e.Name(), "fullchain.cer")
		}
		if info.KeyFile == "" {
			info.KeyFile = filepath.Join(dataDir, e.Name(), domain+".key")
		}
		if n, err := strconv.ParseInt(kv["Le_NextRenewTime"], 10, 64); err == nil {
			info.NextRenew = n
		}
		info.NotAfter = certNotAfter(info.CertFile)

		cur := candidate{info: info, ecc: strings.HasSuffix(e.Name(), "_ecc")}
		if prev, ok := byDomain[info.Domain]; ok {
			// 不比旧的新就不换;打平时留 _ecc——面板自己签发的默认就是 ec-256
			if cur.info.NotAfter < prev.info.NotAfter ||
				(cur.info.NotAfter == prev.info.NotAfter && !cur.ecc) {
				continue
			}
		}
		byDomain[info.Domain] = cur
	}

	certs := make([]CertInfo, 0, len(byDomain))
	for _, c := range byDomain {
		certs = append(certs, c.info)
	}
	sort.Slice(certs, func(i, j int) bool { return certs[i].Domain < certs[j].Domain })
	return certs, nil
}

// ManagedCertDir 返回面板为该域名安装证书的目录。RemoveCert 会把它整个删掉,
// 所以「设置里的证书路径落在这个目录下」也算正在使用(删除守卫要靠它兜住
// 域名与路径分岔的情形)。
func ManagedCertDir(domain string) string {
	return filepath.Join(certBaseDir, domain)
}

// RemoveCert 删除一张证书:先让 acme.sh 忘掉它(不再续期),再删掉安装出去的文件副本。
//
// 调用方必须先确认没有服务在用这个域名——面板/订阅正用着的证书被删掉,下次重启就起不来。
// 那个检查放在调用方(它才读得到设置),这里只管删。
// 入站 TLS 是否手填了这两个路径不做检查:那要遍历所有 TLS 配置解析 JSON、还要处理软链接
// 和相对路径,代价和收益不成正比,由前端在确认框里提示。
func (a *AcmeService) RemoveCert(domain string) error {
	if runtime.GOOS == "windows" {
		return common.NewError("Windows 不支持 acme.sh")
	}
	domain = strings.TrimSpace(domain)
	// 放行通配符:DNS-01 签的通配符证书也得能删,否则列表里那行永远管不了。
	// 域名只进 exec 的参数切片和 filepath.Join,不经过 shell,字面 '*' 是安全的。
	if domain == "" || !validCertDomain(domain) {
		return common.NewErrorf("域名无效: %q", domain)
	}

	bin, home := resolveAcmeSh()
	if bin == "" {
		return common.NewError("未安装 acme.sh,无法删除证书")
	}
	// --remove 只是让 acme.sh 停止续期并移除它的记录,不会碰安装出去的文件
	if out, err := runCmd(cmdDetectTO, home, bin, "--remove", "-d", domain); err != nil {
		return common.NewErrorf("acme.sh 移除记录失败:\n%s", out)
	}
	// acme.sh 会把证书目录留在原地(它自己的文档也这么说),这里一并清掉,
	// 否则列表里那条会因为目录还在而阴魂不散。
	// 目录在 acme.sh 的家目录下(可执行文件所在处),不是 home 那个上级目录。
	dataDir := filepath.Dir(bin)
	for _, dir := range []string{
		filepath.Join(dataDir, domain),
		filepath.Join(dataDir, domain+"_ecc"),
	} {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			if err := os.RemoveAll(dir); err != nil {
				logger.Warning("已从 acme.sh 移除 ", domain, ",但删除目录失败 ", dir, ": ", err)
			}
		}
	}
	// 安装到 /root/cert/<域名>/ 的那份也删掉:留着会让人以为证书还在正常轮换,
	// 实际上已经没人续期了。用户自己指定到别处的路径不碰(不在 certBaseDir 下)。
	installed := filepath.Join(certBaseDir, domain)
	if st, err := os.Stat(installed); err == nil && st.IsDir() {
		if err := os.RemoveAll(installed); err != nil {
			logger.Warning("删除已安装的证书目录失败 ", installed, ": ", err)
		}
	}
	logger.Info("已删除证书:", domain)
	return nil
}
