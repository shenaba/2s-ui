# 2S-UI
[English](README.md) · [فارسی](README.fa.md) · [Tiếng Việt](README.vi.md) · [简体中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md) · [Русский](README.ru.md)

**基于 SagerNet/Sing-Box 的多协议代理 Web 面板，支持订阅分发、流量监控与自托管部署。**

![](https://img.shields.io/github/v/release/shenaba/2s-ui.svg)
[![Docker Pulls](https://img.shields.io/docker/pulls/shenaba/2s-ui.svg)](https://hub.docker.com/r/shenaba/2s-ui)
[![Go Report Card](https://goreportcard.com/badge/github.com/shenaba/2s-ui)](https://goreportcard.com/report/github.com/shenaba/2s-ui)
[![Downloads](https://img.shields.io/github/downloads/shenaba/2s-ui/total.svg)](https://github.com/shenaba/2s-ui/releases)
[![License](https://img.shields.io/badge/license-GPL%20V3-blue.svg?longCache=true)](https://www.gnu.org/licenses/gpl-3.0.en.html)

> **免责声明：** 本项目仅供个人学习与交流使用，请勿用于非法用途，请勿用于生产环境。

**如果你觉得这个项目有帮助，可以给一个** :star2:

**想参与贡献？** 请查看 [CONTRIBUTING.md](CONTRIBUTING.md)，了解开发环境、代码规范、测试和 Pull Request 流程。

2S-UI 基于 [alireza0/s-ui](https://github.com/alireza0/s-ui) 继续维护，在保留原项目设计方向的基础上，持续更新 sing-box 支持、多协议能力、部署脚本与问题修复。感谢原作者及贡献者的开源工作。

## 快速概览
| 功能 | 是否支持 |
| ---- | :------: |
| 多协议 | :heavy_check_mark: |
| 多语言 | :heavy_check_mark: |
| 多客户端/入站 | :heavy_check_mark: |
| 高级流量路由界面 | :heavy_check_mark: |
| 客户端、流量与系统状态 | :heavy_check_mark: |
| 订阅链接（link/json/clash + info） | :heavy_check_mark: |
| **域名自动申请证书（ACME / Let's Encrypt）** ✨ | :heavy_check_mark: |
| 深色/浅色主题 | :heavy_check_mark: |
| API 接口 | :heavy_check_mark: |

## 支持平台
| 平台 | 架构 | 状态 |
| ---- | ---- | ---- |
| Linux | amd64, arm64, armv7, armv6, armv5, 386, s390x | 支持 |
| Windows | amd64, 386, arm64 | 支持 |
| macOS | amd64, arm64 | 实验性 |

## 截图

!["Main"](https://github.com/shenaba/2s-ui-frontend/raw/main/media/main.png)

[更多 UI 截图](https://github.com/shenaba/2s-ui-frontend/blob/main/screenshots.md)

## API 文档

[API Documentation Wiki](https://github.com/shenaba/2s-ui/wiki/API-Documentation)

## 默认安装信息
- 面板端口：2095
- 面板路径：/app/
- 订阅端口：2096
- 订阅路径：/sub/
- 用户名/密码：admin

## 安装或升级到最新版本

### Linux/macOS
```sh
bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
```

### Windows
1. 从 [GitHub Releases](https://github.com/shenaba/2s-ui/releases/latest) 下载最新 Windows 版本
2. 解压 ZIP 文件
3. 以管理员身份运行 `install-windows.bat`
4. 按安装向导操作

## 安装历史版本

**步骤 1：** 如果要安装指定历史版本，请在安装命令末尾添加版本号。例如版本 `1.0.0`：

```sh
VERSION=1.0.0 && bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/$VERSION/install.sh) $VERSION
```

## 手动安装

### Linux/macOS
1. 根据你的系统和架构，从 GitHub 下载最新版本：[https://github.com/shenaba/2s-ui/releases/latest](https://github.com/shenaba/2s-ui/releases/latest)
2. **可选：** 获取最新的 `s-ui.sh`：[https://raw.githubusercontent.com/shenaba/2s-ui/main/s-ui.sh](https://raw.githubusercontent.com/shenaba/2s-ui/main/s-ui.sh)
3. **可选：** 将 `s-ui.sh` 复制到 `/usr/bin/s-ui`，并执行 `chmod +x /usr/bin/s-ui`
4. 解压 `s-ui` 的 tar.gz 文件到你选择的目录，并进入解压目录
5. 将 `*.service` 文件复制到 `/etc/systemd/system/`，并执行 `systemctl daemon-reload`
6. 启用自启动并启动 2S-UI 服务：`systemctl enable s-ui --now`
7. 启动 sing-box 服务：`systemctl enable sing-box --now`

### Windows
1. 从 GitHub 获取最新 Windows 版本：[https://github.com/shenaba/2s-ui/releases/latest](https://github.com/shenaba/2s-ui/releases/latest)
2. 下载对应的 Windows 安装包，例如 `s-ui-windows-amd64.zip`
3. 将 ZIP 文件解压到你选择的目录
4. 以管理员身份运行 `install-windows.bat`
5. 按安装向导操作
6. 通过 http://localhost:2095/app 访问面板

## 卸载 2S-UI

```sh
sudo -i

systemctl disable s-ui  --now

rm -f /etc/systemd/system/sing-box.service
systemctl daemon-reload

rm -fr /usr/local/s-ui
rm /usr/bin/s-ui
```

## 使用 Docker 安装

<details>
   <summary>点击展开</summary>

### 使用方式

**步骤 1：** 安装 Docker

```shell
curl -fsSL https://get.docker.com | sh
```

**步骤 2：** 安装 2S-UI

> Docker Compose 方式

```shell
mkdir 2s-ui && cd 2s-ui
wget -q https://raw.githubusercontent.com/shenaba/2s-ui/main/docker-compose.yml
docker compose up -d
```

> Docker 方式

```shell
mkdir 2s-ui && cd 2s-ui
docker run -itd \
    -p 2095:2095 -p 2096:2096 -p 443:443 \
    -v $PWD/db/:/app/db/ \
    -v $PWD/cert/:/root/cert/ \
    --name s-ui --restart=unless-stopped \
    ghcr.io/shenaba/2s-ui:latest
```

> 自行构建镜像

```shell
git clone https://github.com/shenaba/2s-ui
git submodule update --init --recursive
docker build -t 2s-ui .
```

</details>

## 手动运行（贡献开发）

<details>
   <summary>点击展开</summary>

### 构建并运行完整项目
```shell
./runSUI.sh
```

### 克隆仓库
```shell
# 克隆仓库
git clone https://github.com/shenaba/2s-ui
# 克隆子模块
git submodule update --init --recursive
```

### - 前端

前端代码请查看 [2s-ui-frontend](https://github.com/shenaba/2s-ui-frontend)

### - 后端
> 请先构建一次前端。

构建后端：
```shell
# 删除旧的前端编译文件
rm -fr web/html/*
# 应用新的前端编译文件
cp -R frontend/dist/ web/html/
# 构建
go build -o sui main.go
```

从仓库根目录运行后端：
```shell
./sui
```

</details>

## 语言

- English
- Farsi
- Vietnamese
- Chinese (Simplified)
- Chinese (Traditional)
- Russian

## 功能

- 支持的协议：
  - 通用协议：Mixed, SOCKS, HTTP, HTTPS, Direct, Redirect, TProxy
  - V2Ray 系列：VLESS, VMess, Trojan, Shadowsocks
  - 其他协议：ShadowTLS, Hysteria, Hysteria2, Naive, TUIC
- 支持 XTLS 协议
- 提供高级流量路由界面，支持 PROXY Protocol、External、Transparent Proxy、SSL Certificate 和 Port
- 提供高级入站和出站配置界面
- 支持客户端流量限制和到期时间
- 展示在线客户端、入站、出站、流量统计和系统状态监控
- 订阅服务支持添加外部链接和订阅
- 支持通过自备域名和 SSL 证书，为 Web 面板和订阅服务启用 HTTPS
- **域名自动申请证书** —— 只需填写域名，2S-UI 即自动签发并自动续期免费的 Let's Encrypt 证书（无需 certbot，无需定时任务）
- 支持深色/浅色主题

## 环境变量

<details>
  <summary>点击展开</summary>

### 使用方式

| 变量 | 类型 | 默认值 |
| ---- | :--: | :---- |
| SUI_LOG_LEVEL | `"debug"` \| `"info"` \| `"warn"` \| `"error"` | `"info"` |
| SUI_DEBUG | `boolean` | `false` |
| SUI_BIN_FOLDER | `string` | `"bin"` |
| SUI_DB_FOLDER | `string` | `"db"` |
| SINGBOX_API | `string` | - |

</details>

## SSL 证书

### 🔐 域名自动申请证书（ACME / Let's Encrypt）—— 推荐

只需在**面板设置**里填写域名（证书模式选 **ACME**），2S-UI 即自动签发并自动续期
免费的 Let's Encrypt 证书，无需 certbot、无需定时任务。Web 面板与订阅服务可分别
独立启用。配置成功后即可通过 `https://<你的域名>:2095/app` 访问面板。

> 需要 TCP **80** 端口可从公网访问（HTTP-01 校验）。Docker 部署映射 80 端口：docker compose 方式请取消 `docker-compose.yml` 中 `80:80` 那一行的注释；docker run 方式请加上 `-p 80:80`。
> 证书保存在 `cert/` 目录，重启后保留。若域名/端口配置有误，会自动回退 HTTP。

<details>
  <summary>想自己管理证书？（Certbot）</summary>

### Certbot

```bash
snap install core; snap refresh core
snap install --classic certbot
ln -s /snap/bin/certbot /usr/bin/certbot

certbot certonly --standalone --register-unsafely-without-email --non-interactive --agree-tos -d <Your Domain Name>
```

</details>

## Stargazers over Time
[![Stargazers over time](https://starchart.cc/shenaba/2s-ui.svg)](https://starchart.cc/shenaba/2s-ui)
