package cmd

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/shenaba/2s-ui/config"
	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/service"
	"github.com/shenaba/2s-ui/util"

	"github.com/shirou/gopsutil/v4/net"
)

func resetSetting() {
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		fmt.Println(err)
		return
	}

	settingService := service.SettingService{}
	err = settingService.ResetSettings()
	if err != nil {
		fmt.Println("reset setting failed:", err)
	} else {
		fmt.Println("reset setting success")
	}
}

func updateSetting(port int, path string, subPort int, subPath string) {
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		fmt.Println(err)
		return
	}

	settingService := service.SettingService{}

	if port > 0 {
		err := settingService.SetPort(port)
		if err != nil {
			fmt.Println("set port failed:", err)
		} else {
			fmt.Println("set port success")
		}
	}
	if path != "" {
		err := settingService.SetWebPath(path)
		if err != nil {
			fmt.Println("set path failed:", err)
		} else {
			fmt.Println("set path success")
		}
	}
	if subPort > 0 {
		err := settingService.SetSubPort(subPort)
		if err != nil {
			fmt.Println("set sub port failed:", err)
		} else {
			fmt.Println("set sub port success")
		}
	}
	if subPath != "" {
		err := settingService.SetSubPath(subPath)
		if err != nil {
			fmt.Println("set sub path failed:", err)
		} else {
			fmt.Println("set sub path success")
		}
	}
}

func showSetting() {
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		fmt.Println(err)
		return
	}
	settingService := service.SettingService{}
	allSetting, err := settingService.GetAllSetting()
	if err != nil {
		fmt.Println("get current port failed,error info:", err)
	}
	fmt.Println("Current panel settings:")
	fmt.Println("\tPanel port:\t", (*allSetting)["webPort"])
	fmt.Println("\tPanel path:\t", (*allSetting)["webPath"])
	if (*allSetting)["webListen"] != "" {
		fmt.Println("\tPanel IP:\t", (*allSetting)["webListen"])
	}
	if (*allSetting)["webDomain"] != "" {
		fmt.Println("\tPanel Domain:\t", (*allSetting)["webDomain"])
	}
	if (*allSetting)["webURI"] != "" {
		fmt.Println("\tPanel URI:\t", (*allSetting)["webURI"])
	}
	fmt.Println()
	fmt.Println("Current subscription settings:")
	fmt.Println("\tSub port:\t", (*allSetting)["subPort"])
	fmt.Println("\tSub path:\t", (*allSetting)["subPath"])
	if (*allSetting)["subListen"] != "" {
		fmt.Println("\tSub IP:\t", (*allSetting)["subListen"])
	}
	if (*allSetting)["subDomain"] != "" {
		fmt.Println("\tSub Domain:\t", (*allSetting)["subDomain"])
	}
	if (*allSetting)["subURI"] != "" {
		fmt.Println("\tSub URI:\t", (*allSetting)["subURI"])
	}
}

func getPublicIP() string {
	apis := []string{
		"https://api64.ipify.org",
		"https://ip.sb",
		"https://icanhazip.com",
		"https://ipinfo.io/ip",
		"https://checkip.amazonaws.com",
	}
	type result struct {
		ip  string
		err error
	}
	ch := make(chan result, len(apis))
	var wg sync.WaitGroup
	client := &http.Client{Timeout: 3 * time.Second}

	for _, api := range apis {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			resp, err := client.Get(url)
			if err != nil {
				ch <- result{"", err}
				return
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				ch <- result{"", err}
				return
			}
			ch <- result{string(body), nil}
		}(api)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	for res := range ch {
		if res.err == nil && res.ip != "" {
			return strings.TrimSpace(res.ip)
		}
	}
	return ""
}

// 公网 IP 查询要走外网(每个 API 3s 超时),面板与订阅都可能用到,只查一次。
var (
	publicIPOnce   sync.Once
	publicIPCached string
)

func cachedPublicIP() string {
	publicIPOnce.Do(func() { publicIPCached = getPublicIP() })
	return publicIPCached
}

// printAddresses 按「显式 URI → 域名 → 监听 IP → 枚举本机网卡 + 公网 IP」的顺序
// 打印一组可访问地址,缩进一级挂在调用方打印的标题下。
// 面板与订阅的推断规则完全相同,区别只在取哪组设置,故共用此函数。
func printAddresses(uri, domain, listen, path string, port int, tls bool) {
	// 手工设置的对外地址优先,不再推断:反代终结 TLS 时,服务自身的协议/端口跟对外
	// 地址无关,推断必然是错的。与前端 restartApp、GetFinalSubURI 的取值顺序一致。
	if uri != "" {
		fmt.Println("  " + uri)
		return
	}
	proto := "http://"
	if tls {
		proto = "https://"
	}
	portText := fmt.Sprintf(":%d", port)
	if (port == 443 && tls) || (port == 80 && !tls) {
		portText = ""
	}
	// 域名和监听地址都是设置里的原始输入,IPv6 字面量不带方括号,直接拼进 URL 会让
	// 端口被当成又一段 hextet。与下面枚举网卡时手工补的方括号是同一件事。
	if domain != "" {
		fmt.Println("  " + proto + util.HostForURI(domain) + portText + path)
		return
	}
	if listen != "" {
		fmt.Println("  " + proto + util.HostForURI(listen) + portText + path)
		return
	}
	fmt.Println("  Local address:")
	netInterfaces, _ := net.Interfaces()
	for i := 0; i < len(netInterfaces); i++ {
		if len(netInterfaces[i].Flags) > 2 && netInterfaces[i].Flags[0] == "up" && netInterfaces[i].Flags[1] != "loopback" {
			addrs := netInterfaces[i].Addrs
			for _, address := range addrs {
				IP := strings.Split(address.Addr, "/")[0]
				if strings.Contains(address.Addr, ".") {
					fmt.Println("    " + proto + IP + portText + path)
				} else if !strings.HasPrefix(address.Addr, "fe80::") {
					fmt.Println("    " + proto + "[" + IP + "]" + portText + path)
				}
			}
		}
	}
	if pubIP := cachedPublicIP(); pubIP != "" {
		fmt.Println("  Global address:")
		fmt.Println("    " + proto + pubIP + portText + path)
	}
}

func getPanelURI() {
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		fmt.Println(err)
		return
	}
	settingService := service.SettingService{}

	webURI, _ := settingService.GetWebURI()
	webDomain, _ := settingService.GetWebDomain()
	webListen, _ := settingService.GetListen()
	webPath, _ := settingService.GetWebPath()
	webPort, _ := settingService.GetPort()
	webCert, _ := settingService.GetCertFile()
	webKey, _ := settingService.GetKeyFile()
	webCertMode, _ := settingService.GetWebCertMode()
	webNginx, _ := settingService.GetWebNginx()

	fmt.Println("Panel:")
	// 反代模式下没设对外地址,推断出来的是面板自身的内网地址,不是用户该访问的
	if webNginx && webURI == "" {
		fmt.Println("  Note: TLS is terminated by a reverse proxy, so the address below is")
		fmt.Println("        the panel's own, not the public one. Set \"Panel URI\" in the")
		fmt.Println("        panel settings to have this command report the public address.")
	}
	// TLS 判定对齐 web.go 的实际分支【优先级】:webNginx 最先短路,面板只跑 HTTP、
	// 根本不看证书字段;其后才是 acme 模式,或证书/私钥任一非空即尝试 TLS(而非要求
	// 两者都填——只填一个是坏配置,服务端会直接报错,不该在这里显示成 http)。
	// 漏掉 webNginx 这一层会真出错:反代模式下前端把证书路径输入框隐藏了,用户没途径
	// 清掉旧值,于是残留路径会让这里报 https,而面板实际在跑 http。
	printAddresses(webURI, webDomain, webListen, webPath, webPort,
		!webNginx && (webCertMode == "acme" || webCert != "" || webKey != ""))

	subURI, _ := settingService.GetSubURI()
	subDomain, _ := settingService.GetSubDomain()
	subListen, _ := settingService.GetSubListen()
	subPath, _ := settingService.GetSubPath()
	subPort, _ := settingService.GetSubPort()
	subCert, _ := settingService.GetSubCertFile()
	subKey, _ := settingService.GetSubKeyFile()
	subCertMode, _ := settingService.GetSubCertMode()
	subNginx, _ := settingService.GetSubNginx()

	fmt.Println()
	fmt.Println("Subscription:")
	// 反代模式下同面板侧:推断出的是订阅自身的内网地址,不是发给客户端的那个
	if subNginx && subURI == "" {
		fmt.Println("  Note: TLS is terminated by a reverse proxy, so the address below is")
		fmt.Println("        the sub server's own, not the public one. Set \"Sub URI\" in the")
		fmt.Println("        panel settings to have this command report the public address.")
	}
	// 同上,对齐 sub.go 的判定【优先级】:subNginx 最先短路,订阅只跑明文 HTTP
	printAddresses(subURI, subDomain, subListen, subPath, subPort,
		!subNginx && (subCertMode == "acme" || subCert != "" || subKey != ""))
}
