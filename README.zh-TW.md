# 2S-UI

[English](README.md) · [فارسی](README.fa.md) · [Tiếng Việt](README.vi.md) · [简体中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md) · [Русский](README.ru.md)

**基於 SagerNet/Sing-Box 的多協定代理 Web 面板，支援訂閱分發、流量監控與自架部署。**

[English](README.md)

![](https://img.shields.io/github/v/release/shenaba/2s-ui.svg)
[![Docker Pulls](https://img.shields.io/docker/pulls/shenaba/2s-ui.svg)](https://hub.docker.com/r/shenaba/2s-ui)
[![Go Report Card](https://goreportcard.com/badge/github.com/shenaba/2s-ui)](https://goreportcard.com/report/github.com/shenaba/2s-ui)
[![Downloads](https://img.shields.io/github/downloads/shenaba/2s-ui/total.svg)](https://github.com/shenaba/2s-ui/releases)
[![License](https://img.shields.io/badge/license-GPL%20V3-blue.svg?longCache=true)](https://www.gnu.org/licenses/gpl-3.0.en.html)

> **免責聲明：** 本專案僅供個人學習與交流使用，請勿用於非法用途，請勿用於正式環境。

**如果你覺得這個專案有幫助，可以給一個** :star2:

**想參與貢獻？** 請查看 [CONTRIBUTING.md](CONTRIBUTING.md)，瞭解開發環境、程式碼規範、測試與 Pull Request 流程。

2S-UI 基於 [alireza0/s-ui](https://github.com/alireza0/s-ui) 繼續維護，在保留原專案設計方向的基礎上，持續更新 sing-box 支援、多協定能力、部署指令稿與問題修復。感謝原作者及貢獻者的開源工作。

## 快速概覽
| 功能 | 是否支援 |
| ---- | :------: |
| 多協定 | :heavy_check_mark: |
| 多語言 | :heavy_check_mark: |
| 多用戶端/入站 | :heavy_check_mark: |
| 進階流量路由介面 | :heavy_check_mark: |
| 用戶端、流量與系統狀態 | :heavy_check_mark: |
| 訂閱連結（link/json/clash + info） | :heavy_check_mark: |
| **網域自動申請憑證（ACME / Let's Encrypt）** ✨ | :heavy_check_mark: |
| 深色/淺色佈景主題 | :heavy_check_mark: |
| API 介面 | :heavy_check_mark: |

## 支援平台
| 平台 | 架構 | 狀態 |
| ---- | ---- | ---- |
| Linux | amd64, arm64, armv7, armv6, armv5, 386, s390x | 支援 |
| Windows | amd64, 386, arm64 | 支援 |
| macOS | amd64, arm64 | 實驗性 |

## 螢幕截圖

!["Main"](https://github.com/shenaba/2s-ui-frontend/raw/main/media/main.png)

[更多 UI 截圖](https://github.com/shenaba/2s-ui-frontend/blob/main/screenshots.md)

## API 文件

[API Documentation Wiki](https://github.com/shenaba/2s-ui/wiki/API-Documentation)

## 預設安裝資訊
- 面板連接埠：2095
- 面板路徑：/app/
- 訂閱連接埠：2096
- 訂閱路徑：/sub/
- 使用者名稱/密碼：admin

## 安裝或升級到最新版本

### Linux/macOS
```sh
bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
```

### Windows
1. 從 [GitHub Releases](https://github.com/shenaba/2s-ui/releases/latest) 下載最新 Windows 版本
2. 解壓縮 ZIP 檔案
3. 以系統管理員身分執行 `install-windows.bat`
4. 依安裝精靈操作

## 安裝歷史版本

**步驟 1：** 如果要安裝指定歷史版本，請在安裝指令末尾加上版本號。例如版本 `1.0.0`：

```sh
VERSION=1.0.0 && bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/$VERSION/install.sh) $VERSION
```

## 手動安裝

### Linux/macOS
1. 根據你的系統與架構，從 GitHub 下載最新版本：[https://github.com/shenaba/2s-ui/releases/latest](https://github.com/shenaba/2s-ui/releases/latest)
2. **可選：** 取得最新的 `s-ui.sh`：[https://raw.githubusercontent.com/shenaba/2s-ui/main/s-ui.sh](https://raw.githubusercontent.com/shenaba/2s-ui/main/s-ui.sh)
3. **可選：** 將 `s-ui.sh` 複製到 `/usr/bin/s-ui`，並執行 `chmod +x /usr/bin/s-ui`
4. 將 `s-ui` 的 tar.gz 檔案解壓縮到你選擇的目錄，並進入解壓縮後的目錄
5. 將 `*.service` 檔案複製到 `/etc/systemd/system/`，並執行 `systemctl daemon-reload`
6. 啟用開機自動啟動並啟動 2S-UI 服務：`systemctl enable s-ui --now`
7. 啟動 sing-box 服務：`systemctl enable sing-box --now`

### Windows
1. 從 GitHub 取得最新 Windows 版本：[https://github.com/shenaba/2s-ui/releases/latest](https://github.com/shenaba/2s-ui/releases/latest)
2. 下載對應的 Windows 安裝包，例如 `s-ui-windows-amd64.zip`
3. 將 ZIP 檔案解壓縮到你選擇的目錄
4. 以系統管理員身分執行 `install-windows.bat`
5. 依安裝精靈操作
6. 透過 http://localhost:2095/app 存取面板

## 解除安裝 2S-UI

```sh
sudo -i

systemctl disable s-ui  --now

rm -f /etc/systemd/system/sing-box.service
systemctl daemon-reload

rm -fr /usr/local/s-ui
rm /usr/bin/s-ui
```

## 使用 Docker 安裝

<details>
   <summary>點擊展開</summary>

### 使用方式

**步驟 1：** 安裝 Docker

```shell
curl -fsSL https://get.docker.com | sh
```

**步驟 2：** 安裝 2S-UI

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

> 自行建置映像檔

```shell
git clone https://github.com/shenaba/2s-ui
git submodule update --init --recursive
docker build -t 2s-ui .
```

</details>

## 手動執行（貢獻開發）

<details>
   <summary>點擊展開</summary>

### 建置並執行完整專案
```shell
./runSUI.sh
```

### 複製儲存庫
```shell
# 複製儲存庫
git clone https://github.com/shenaba/2s-ui
# 複製子模組
git submodule update --init --recursive
```

### - 前端

前端程式碼請查看 [2s-ui-frontend](https://github.com/shenaba/2s-ui-frontend)

### - 後端
> 請先建置一次前端。

建置後端：
```shell
# 刪除舊的前端編譯檔案
rm -fr web/html/*
# 套用新的前端編譯檔案
cp -R frontend/dist/ web/html/
# 建置
go build -o sui main.go
```

從儲存庫根目錄執行後端：
```shell
./sui
```

</details>

## 語言

- English
- Farsi
- Vietnamese
- Chinese (Simplified)
- Chinese (Traditional)
- Russian

## 功能

- 支援的協定：
  - 通用協定：Mixed, SOCKS, HTTP, HTTPS, Direct, Redirect, TProxy
  - V2Ray 系列：VLESS, VMess, Trojan, Shadowsocks
  - 其他協定：ShadowTLS, Hysteria, Hysteria2, Naive, TUIC
- 支援 XTLS 協定
- 提供進階流量路由介面，支援 PROXY Protocol、External、Transparent Proxy、SSL Certificate 與 Port
- 提供進階入站與出站設定介面
- 支援用戶端流量限制與到期時間
- 顯示線上用戶端、入站、出站、流量統計與系統狀態監控
- 訂閱服務支援新增外部連結與訂閱
- 支援透過自備網域與 SSL 憑證，為 Web 面板與訂閱服務啟用 HTTPS
- **網域自動申請憑證** —— 只需填寫網域，2S-UI 即自動簽發並自動續期免費的 Let's Encrypt 憑證（無需 certbot，無需排程工作）
- 支援深色/淺色佈景主題

## 環境變數

<details>
  <summary>點擊展開</summary>

### 使用方式

| 變數 | 類型 | 預設值 |
| ---- | :--: | :---- |
| SUI_LOG_LEVEL | `"debug"` \| `"info"` \| `"warn"` \| `"error"` | `"info"` |
| SUI_DEBUG | `boolean` | `false` |
| SUI_BIN_FOLDER | `string` | `"bin"` |
| SUI_DB_FOLDER | `string` | `"db"` |
| SINGBOX_API | `string` | - |

</details>

## SSL 憑證

### 🔐 網域自動申請憑證（ACME / Let's Encrypt）—— 推薦

只需在**面板設定**裡填寫網域（憑證模式選 **ACME**），2S-UI 即自動簽發並自動續期
免費的 Let's Encrypt 憑證，無需 certbot、無需排程工作。Web 面板與訂閱服務可分別
獨立啟用。設定成功後即可透過 `https://<你的網域>:2095/app` 存取面板。

> 需要 TCP **80** 連接埠可從公網存取（HTTP-01 校驗）。Docker 部署對應 80 連接埠：docker compose 方式請取消 `docker-compose.yml` 中 `80:80` 該行的註解；docker run 方式請加上 `-p 80:80`。
> 憑證儲存在 `cert/` 目錄，重新啟動後保留。若網域/連接埠設定有誤，會自動回復 HTTP。

<details>
  <summary>想自己管理憑證？（Certbot）</summary>

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
