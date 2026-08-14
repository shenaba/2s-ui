# <img src="frontend/public/assets/favicon.svg" width="44" height="44" align="texttop" alt=""> 2S-UI
[English](README.md) · [فارسی](README.fa.md) · [Tiếng Việt](README.vi.md) · [简体中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md) · [Русский](README.ru.md)

2S-UI 是一个开源的 [Sing-Box](https://github.com/SagerNet/sing-box) 管理面板，面向自建代理服务的部署与运维。协议配置、路由规则、用户与订阅、流量统计集中于同一套界面，支持六种语言与深浅双主题；单机即可运行，多台亦可组成集群统一管理。

2S-UI 由 [s-ui](https://github.com/alireza0/s-ui) 分支而来，重写了整套前端，并引入多节点集群、ACME 证书自动签发与续期、面板内一键升级，以及不中断在线连接的用户热更新。

![](https://img.shields.io/github/v/release/shenaba/2s-ui.svg)
[![Docker Pulls](https://img.shields.io/docker/pulls/shenaba/2s-ui.svg)](https://hub.docker.com/r/shenaba/2s-ui)
[![Go Report Card](https://goreportcard.com/badge/github.com/shenaba/2s-ui)](https://goreportcard.com/report/github.com/shenaba/2s-ui)
[![Downloads](https://img.shields.io/github/downloads/shenaba/2s-ui/total.svg)](https://github.com/shenaba/2s-ui/releases)
[![License](https://img.shields.io/badge/license-GPL%20V3-blue.svg?longCache=true)](https://www.gnu.org/licenses/gpl-3.0.en.html)

> **免责声明：** 本项目仅供个人学习与交流使用，请勿用于非法用途，请勿用于生产环境。

**如果你觉得这个项目有帮助，可以给一个** :star2:

## 功能

- **多协议** —— 入站出站支持 VLESS、VMess、Trojan、Shadowsocks、Hysteria2、TUIC、
  AnyTLS 等，另有 WireGuard / WARP / Tailscale 三类 Endpoint（[完整列表](#protocols)）
- **TLS 集中管理** —— Reality、uTLS 指纹、XTLS；证书注册一次，之后按入站选用
- **路由规则** —— 按域名、IP、端口、协议、进程、用户或 rule-set 匹配，可 and/or 组合。
  DNS 有自己独立的一套规则。
- **多入站客户端** —— 一个客户端可挂在多个入站上，各带流量上限和到期时间，超量或过期
  自动禁用
- **配额自动化** —— 可以从首次使用才开始计时，也可以每 N 天自动重置，跑满的客户端到期
  自动恢复
- **每客户端 IP 数限制** —— 限制单个客户端同时使用的来源 IP 数量，超出的会被断开并短暂
  拒绝重连，不依赖 fail2ban
- **用户热更新** —— 改客户端时原地改写入站的用户表，不重建监听器，其他人不掉线
- **订阅链接** —— 支持 `link`、`json`、`clash` 三种格式，回传用量和到期时间，可并入外部
  链接
- **多节点集群** —— 监控其他 2S-UI 面板、跨节点共享用户、各节点线路合并进同一条订阅
  （[详见下文](#多节点集群)）
- **HTTPS 自动化** —— Let's Encrypt 自动签发续期，并自动生成 nginx 反向代理
  （[详见下文](#域名与证书)）
- **一键更新** —— 面板内原地升级，带校验和验证
- **实时仪表盘** —— 系统资源、流量、协议占比、网络吞吐、节点健康，每块卡片都能单独开关
- **账号与语言** —— 支持多个面板管理员、带过期时间的
  [API Token](https://github.com/shenaba/2s-ui/wiki/API-Documentation)、深色/浅色主题、六种语言

<details id="protocols">
  <summary>支持的协议</summary>

- 通用协议：Mixed, SOCKS, HTTP/HTTPS, Direct, Tun, Redirect, TProxy
- V2Ray 系列：VLESS, VMess, Trojan, Shadowsocks（支持 `plugin` / `plugin_opts`）
- 其他协议：ShadowTLS, Hysteria, Hysteria2, Naive¹, TUIC, AnyTLS
- 仅出站：Tor, SSH, Selector, URLTest
- Endpoints：WireGuard、WARP、Tailscale——可单个测延迟，也可一键测全部
- 支持 XTLS 协议，出站表单支持 Hysteria 端口跳跃

<sup>1</sup> Naive 依赖 cronet 工具链，并非所有平台都能编译：官方 Linux 发布版只在
amd64、arm64、armv7 和 386 上带这个协议。在 armv6、armv5、s390x 上使用 Naive 出站会提示
该二进制未编译此协议。

</details>

<details>
  <summary>语言</summary>

English · Farsi · Vietnamese · Chinese (Simplified) · Chinese (Traditional) · Russian

</details>

<details>
  <summary>截图</summary>

!["Main"](frontend/media/main.png)

更多截图：[frontend/screenshots.md](frontend/screenshots.md)

</details>

## 安装

### Linux/macOS

```sh
bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
```

装好后访问 `http://<你的服务器>:2095/app/`，用 `admin` / `admin` 登录。

| | 默认值 |
| --- | --- |
| 面板 | 端口 `2095`，路径 `/app/` |
| 订阅 | 端口 `2096`，路径 `/sub/` |
| 用户名 / 密码 | `admin` / `admin` |

同时支持 systemd 和 OpenRC（Alpine），安装脚本会自动选择。脚本支持与面板相同的六种
语言：`en`、`fa`、`ru`、`vi`、`zhcn`、`zhtw`，默认跟随系统 `$LANG`，也可以手动指定，
之后 `s-ui` 菜单会沿用该语言：

```sh
SUI_LANG=zhcn bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
```

### Windows

1. 从 [GitHub Releases](https://github.com/shenaba/2s-ui/releases/latest) 下载最新 Windows 版本
2. 解压 ZIP 文件
3. 以管理员身份运行 `install-windows.bat`
4. 按安装向导操作
5. 通过 http://localhost:2095/app 访问面板

### Docker

```shell
mkdir 2s-ui && cd 2s-ui
wget -q https://raw.githubusercontent.com/shenaba/2s-ui/main/docker-compose.yml
docker compose up -d
```

<details>
  <summary>不用 compose，或自行构建镜像</summary>

如果还没装 Docker：

```shell
curl -fsSL https://get.docker.com | sh
```

直接用 `docker run`：

```shell
mkdir 2s-ui && cd 2s-ui
docker run -itd \
    -p 2095:2095 -p 2096:2096 -p 443:443 \
    -v $PWD/db/:/app/db/ \
    -v $PWD/cert/:/root/cert/ \
    --name s-ui --restart=unless-stopped \
    ghcr.io/shenaba/2s-ui:latest
```

自行构建镜像：

```shell
git clone https://github.com/shenaba/2s-ui
docker build -t 2s-ui .
```

</details>

<details>
  <summary>安装指定历史版本、手动安装、卸载</summary>

**安装指定历史版本。** 在安装命令末尾添加版本号，例如 `v1.5.5`：

```sh
VERSION=v1.5.5 && bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/$VERSION/install.sh) $VERSION
```

**手动安装 —— Linux/macOS**

1. 根据你的系统和架构，从 GitHub 下载最新版本：[https://github.com/shenaba/2s-ui/releases/latest](https://github.com/shenaba/2s-ui/releases/latest)
2. **可选：** 获取最新的 `s-ui.sh`：[https://raw.githubusercontent.com/shenaba/2s-ui/main/s-ui.sh](https://raw.githubusercontent.com/shenaba/2s-ui/main/s-ui.sh)
3. **可选：** 将 `s-ui.sh` 复制到 `/usr/bin/s-ui`，并执行 `chmod +x /usr/bin/s-ui`
4. 解压 `s-ui` 的 tar.gz 文件到你选择的目录，并进入解压目录
5. 将 `*.service` 文件复制到 `/etc/systemd/system/`，并执行 `systemctl daemon-reload`
6. 启用自启动并启动 2S-UI 服务：`systemctl enable s-ui --now`
7. 启动 sing-box 服务：`systemctl enable sing-box --now`

**手动安装 —— Windows**

1. 从 GitHub 获取最新 Windows 版本：[https://github.com/shenaba/2s-ui/releases/latest](https://github.com/shenaba/2s-ui/releases/latest)
2. 下载对应的 Windows 安装包，例如 `s-ui-windows-amd64.zip`
3. 将 ZIP 文件解压到你选择的目录
4. 以管理员身份运行 `install-windows.bat`
5. 按安装向导操作
6. 通过 http://localhost:2095/app 访问面板

**卸载**

```sh
sudo -i

systemctl disable s-ui  --now

rm -f /etc/systemd/system/sing-box.service
systemctl daemon-reload

rm -fr /usr/local/s-ui
rm /usr/bin/s-ui
```

</details>

### 升级

有新版本会在侧边栏的版本标签上提示 —— 检查在浏览器端完成，所以面板所在的服务器访问
不了 GitHub 也会提示。Linux 上（systemd 或 Docker）点一下即可原地升级：面板会下载新
版本，用官方发布的 `SHA256SUMS` 校验，先试跑一次新二进制，再替换并重启。不用 SSH。

> Windows 上运行中的 `.exe` 无法自我替换，版本标签只会跳转到 release 页面。
> Docker 里新二进制写在容器可写层：`docker restart` 后仍在，但重建容器会退回镜像
> 自带的版本——想长期生效请拉取新镜像。

### 支持平台

| 平台 | 架构 | 状态 |
| ---- | ---- | ---- |
| Linux | amd64, arm64, armv7, armv6, armv5, 386, s390x | 支持 |
| Windows | amd64, 386, arm64 | 支持 |
| macOS | amd64, arm64 | 实验性 |

## 多节点集群

一个面板可以管理其余面板。在 **节点**（Nodes）页面填上远程 2S-UI 实例的地址和 API Token，
主控面板就会：

- **监控它** —— 5 秒一次心跳，报告每个节点为在线、离线或内核已停止（面板可达但
  sing-box 未运行）。
- **与它共享用户** —— 主控上引用了该节点入站的客户端会被下发到节点并持续同步，
  各节点的流量也会汇总回主控的计数。同步限定在 `@cluster` 分组内，因此节点自己的
  本地用户不会被动到。
- **把它的线路并入同一条订阅** —— 一条订阅链接里同时包含主控和所有绑定节点的服务器。

节点就是另一个通过 v2 API（`Token` 请求头）通信的 2S-UI 实例：无需安装 agent，节点侧
唯一要做的就是在它自己的面板里创建这个 API Token，因此已有的面板可以直接接管。从节点
采纳过来的入站在主控上是只读副本 —— 请到它所属的节点上修改。

<details>
  <summary>用 API 驱动节点同步</summary>

`POST <面板路径>apiv2/save`（面板路径默认为 `/app/`，即 `/app/apiv2/save`）只有在请求
带上 `sync=true` 时，才会触发 Web UI 那种立即下发到各节点的行为；不带该参数时，客户端
和入站的改动仍会通过每小时一次的兜底对账收敛。

</details>

## 域名与证书

TLS 相关的配置都集中在面板设置的 **域名与证书** 标签页。面板和订阅服务各自选择自己
的域名，证书路径会跟着所选域名自动确定，不用再手工填文件路径。

**🔐 域名自动申请证书（ACME / Let's Encrypt）—— 推荐。** 填写域名、填上邮箱、点击签发：
2S-UI 即自动签发并自动续期免费的 Let's Encrypt 证书，随后即可通过
`https://<你的域名>:2095/app` 访问面板。需要 TCP **80** 端口可从公网访问（HTTP-01 校验）。
ACME 仅支持 Linux，在 Windows 上会隐藏。

<details>
  <summary>签发的具体过程，以及 Docker 的 80 端口注意事项</summary>

签发底层走 **acme.sh** —— 首次使用时面板会自动帮你装好 acme.sh（以及 standalone 校验
需要的 `socat`），并启用 acme.sh 自带的 cron 做自动续期，你不需要自己配任何定时任务。

校验方式默认为 **auto** —— 80 端口空闲时用 standalone，否则借用正在运行的 nginx，
必要时会在 `/etc/nginx/conf.d` 下自动补一个最小的 `server_name` 配置块。你也可以
显式指定 **standalone** 或 **nginx**。续期时证书会热加载，无需重启。

> Docker 部署映射 80 端口：docker compose 方式请取消 `docker-compose.yml` 中 `80:80`
> 那一行的注释；docker run 方式请加上 `-p 80:80`。证书保存在 `/root/cert/<域名>/` 下，
> 文件名固定为 `fullchain.pem` / `privkey.pem`，重启后保留（上面 Docker 命令里的挂载
> 正是把这个路径映射出来）。若域名/端口配置有误，会自动回退 HTTP。

</details>

<details>
  <summary>使用自备证书</summary>

自己管理的证书 —— Cloudflare 源证书、企业 CA、certbot 签出的证书 —— 可以在同一个
标签页里注册。2S-UI 会校验文件可读、私钥与证书匹配、以及证书确实覆盖该域名；随后
这个域名就能像其他域名一样在「接口」和「订阅」标签页中选用。已注册的证书会包含在
数据库备份里。

想用 Certbot 自己签发：

```bash
snap install core; snap refresh core
snap install --classic certbot
ln -s /snap/bin/certbot /usr/bin/certbot

certbot certonly --standalone --register-unsafely-without-email --non-interactive --agree-tos -d <Your Domain Name>
```

然后把签出的 `fullchain.pem` / `privkey.pem` 注册到 **域名与证书** 标签页。

</details>

<details>
  <summary>位于反向代理之后</summary>

打开 **TLS 由反向代理终止** 开关，2S-UI 会替你写好 vhost：
`/etc/nginx/conf.d/s-ui-proxy-<域名>.conf`，指向面板并带上必要的转发请求头，用
`nginx -t` 校验、reload，任何一步失败都会回滚并原样返回 nginx 自己的报错。订阅服务
也可以放在同一个反代后面。

</details>

## 参与贡献

请查看 [CONTRIBUTING.md](CONTRIBUTING.md)，了解开发环境、代码规范、测试和 Pull Request 流程。

<details>
  <summary>从源码构建并运行</summary>

```shell
git clone https://github.com/shenaba/2s-ui
cd 2s-ui
./runSUI.sh
```

`build.sh` 会构建前端、把产物拷进 `web/html/` 供 `//go:embed` 使用，再带上必需的
build tags 构建二进制；`runSUI.sh` 在此之上直接把它跑起来。手动构建同样要带上那些
tags，详见 [CONTRIBUTING.md](CONTRIBUTING.md)。

</details>

<details>
  <summary>环境变量</summary>

| 变量 | 类型 | 默认值 |
| ---- | :--: | :---- |
| SUI_LOG_LEVEL | `"debug"` \| `"info"` \| `"warn"` \| `"error"` | `"info"` |
| SUI_DEBUG | `boolean` | `false` |
| SUI_DB_FOLDER | `string` | `"db"` |
| SUI_BIN_FOLDER | `string` | `"bin"` |

`SUI_BIN_FOLDER` 仅在从旧的子进程版本迁移数据库时读取；sing-box 已内嵌进二进制，
运行时不存在 `bin/` 目录。

</details>

## 特别感谢

- [@alireza0](https://github.com/alireza0)

## Stargazers over Time
[![Star History Chart](https://api.star-history.com/svg?repos=shenaba/2s-ui&type=Date)](https://star-history.com/#shenaba/2s-ui&Date)
