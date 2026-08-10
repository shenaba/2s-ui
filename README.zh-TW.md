# <img src="frontend/public/assets/favicon.svg" width="44" height="44" align="texttop" alt=""> 2S-UI

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
| **多節點叢集（跨伺服器共用使用者）** ✨ | :heavy_check_mark: |
| **網域自動申請憑證（ACME / Let's Encrypt）** ✨ | :heavy_check_mark: |
| **自動產生 nginx 反向代理** ✨ | :heavy_check_mark: |
| **面板內一鍵更新** ✨ | :heavy_check_mark: |
| 深色/淺色佈景主題 | :heavy_check_mark: |
| API 介面 | :heavy_check_mark: |

## 支援平台
| 平台 | 架構 | 狀態 |
| ---- | ---- | ---- |
| Linux | amd64, arm64, armv7, armv6, armv5, 386, s390x | 支援 |
| Windows | amd64, 386, arm64 | 支援 |
| macOS | amd64, arm64 | 實驗性 |

## 螢幕截圖

!["Main"](frontend/media/main.png)

[更多 UI 截圖](frontend/screenshots.md)

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

同時支援 systemd 與 OpenRC（Alpine），安裝腳本會自動選擇。

安裝腳本支援與面板相同的六種語言：`en`、`fa`、`ru`、`vi`、`zhcn`、`zhtw`。預設跟隨系統 `$LANG`，也可以手動指定，之後 `s-ui` 選單會沿用該語言：

```sh
SUI_LANG=zhtw bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
```

### Windows
1. 從 [GitHub Releases](https://github.com/shenaba/2s-ui/releases/latest) 下載最新 Windows 版本
2. 解壓縮 ZIP 檔案
3. 以系統管理員身分執行 `install-windows.bat`
4. 依安裝精靈操作

### 在面板內升級

裝好之後，有新版本會在側邊欄的版本標籤上提示 —— 檢查在用戶端完成，所以面板所在的伺服器
連不上 GitHub 也會提示。Linux 上（systemd 或 Docker）點一下即可原地升級：面板會下載新
版本，用官方發佈的 `SHA256SUMS` 校驗，先試跑一次新的執行檔，再替換並重新啟動。不必 SSH。

> Windows 上執行中的 `.exe` 無法自我替換，版本標籤只會連到 release 頁面。
> Docker 裡新的執行檔寫在容器可寫層：`docker restart` 後仍在，但重建容器會退回映像
> 檔自帶的版本——想長期生效請拉取新映像檔。

## 安裝歷史版本

**步驟 1：** 如果要安裝指定歷史版本，請在安裝指令末尾加上版本號。例如版本 `v1.5.5`：

```sh
VERSION=v1.5.5 && bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/$VERSION/install.sh) $VERSION
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
```

### - 前端

前端程式碼位於本儲存庫的 [`frontend/`](frontend) 目錄。

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
  - 通用協定：Mixed, SOCKS, HTTP/HTTPS, Direct, Tun, Redirect, TProxy
  - V2Ray 系列：VLESS, VMess, Trojan, Shadowsocks（支援 `plugin` / `plugin_opts`）
  - 其他協定：ShadowTLS, Hysteria, Hysteria2, Naive¹, TUIC, AnyTLS
  - 僅出站：Tor, SSH, Selector, URLTest
  - Endpoints：WireGuard、WARP、Tailscale——可單獨測延遲，也可一鍵測全部

  <sup>1</sup> Naive 依賴 cronet 工具鏈，並非所有平台都能編譯：官方 Linux 發佈版只在
  amd64、arm64、armv7 與 386 上帶這個協定。在 armv6、armv5、s390x 上使用 Naive 出站會提示
  該執行檔未編譯此協定。

- 支援 XTLS 協定
- 提供進階流量路由介面，支援 PROXY Protocol、External、Transparent Proxy、SSL Certificate 與 Port
- 提供進階入站與出站設定介面
- 支援用戶端流量限制與到期時間；可直接在列表中啟用或停用用戶端
- **使用者熱更新** —— 在 VLESS、VMess、Trojan、Shadowsocks、AnyTLS、Hysteria、Hysteria2
  與 TUIC 上，新增、修改或刪除用戶端時原地更新入站的使用者表，不再重建監聽器，其餘使用者
  不會斷線 —— 這對 QUIC 系協定尤其重要，重建監聽器會中斷它們的全部工作階段。其他入站類型
  仍是重新啟動
- 出站表單支援 Hysteria 連接埠跳躍
- 顯示線上用戶端、入站、出站、流量統計與系統狀態監控
- 訂閱服務支援新增外部連結與訂閱
- **多節點叢集** —— 監控其他 2S-UI 面板、跨節點共用使用者，並把各節點線路合併進同一條訂閱（見下文）
- 支援透過自備網域與 SSL 憑證，為 Web 面板與訂閱服務啟用 HTTPS
- **網域自動申請憑證** —— 只需填寫網域，2S-UI 即自動簽發並自動續期免費的 Let's Encrypt 憑證（acme.sh 由面板自動安裝並呼叫，不必自己設定排程工作）
- **自動產生 nginx 反向代理** —— 把面板放到反向代理後方時，2S-UI 會自動寫好並驗證 vhost
- **面板內一鍵更新** —— 以總和檢查碼驗證的 GitHub Release
- 支援深色/淺色佈景主題

## 多節點叢集

一個面板可以管理其餘面板。在 **節點**（Nodes）頁面填上遠端 2S-UI 執行個體的位址與 API Token，
主控面板就會：

- **監控它** —— 5 秒一次心跳，回報每個節點為線上、離線或核心已停止（面板可連線但
  sing-box 未執行）。
- **與它共用使用者** —— 主控上引用該節點入站的用戶端會被下發到節點並持續同步，
  各節點的流量也會彙整回主控的計數。同步限定在 `@cluster` 群組內，因此節點自己的
  本機使用者不會被動到。
- **把它的線路併入同一條訂閱** —— 一條訂閱連結裡同時包含主控與所有繫結節點的伺服器。

節點就是另一個透過 v2 API（`Token` 請求標頭）通訊的 2S-UI 執行個體：不必安裝 agent，
節點端唯一要做的就是在它自己的面板裡建立這個 API Token，因此既有的面板可以直接接管。
從節點採納過來的入站在主控上是唯讀複本 —— 請到它所屬的節點上修改。

## 環境變數

<details>
  <summary>點擊展開</summary>

### 使用方式

| 變數 | 類型 | 預設值 |
| ---- | :--: | :---- |
| SUI_LOG_LEVEL | `"debug"` \| `"info"` \| `"warn"` \| `"error"` | `"info"` |
| SUI_DEBUG | `boolean` | `false` |
| SUI_DB_FOLDER | `string` | `"db"` |
| SUI_BIN_FOLDER | `string` | `"bin"` |

`SUI_BIN_FOLDER` 僅在從舊的子行程版本遷移資料庫時讀取；sing-box 已內嵌進執行檔，
執行時不存在 `bin/` 目錄。

</details>

## 網域與憑證

TLS 相關的設定都集中在面板設定的 **網域與憑證** 分頁。面板與訂閱服務各自選擇自己的
網域，憑證路徑會跟著所選網域自動決定，不必再手動填檔案路徑。

### 🔐 網域自動申請憑證（ACME / Let's Encrypt）—— 推薦

填寫網域、填上電子郵件、按下簽發：2S-UI 即自動簽發並自動續期免費的 Let's Encrypt
憑證。簽發底層走 **acme.sh** —— 首次使用時面板會自動幫你安裝 acme.sh（以及 standalone
驗證需要的 `socat`），並啟用 acme.sh 自帶的 cron 做自動續期，你不需要自己設定任何排程
工作。設定成功後即可透過 `https://<你的網域>:2095/app` 存取面板。

驗證方式預設為 **auto** —— 80 連接埠閒置時用 standalone，否則借用正在執行的 nginx，
必要時會在 `/etc/nginx/conf.d` 下自動補一個最小的 `server_name` 設定區塊。你也可以
明確指定 **standalone** 或 **nginx**。續期時憑證會熱重載，不需重新啟動。

> 需要 TCP **80** 連接埠可從公網存取（HTTP-01 校驗）。Docker 部署對應 80 連接埠：docker compose 方式請取消 `docker-compose.yml` 中 `80:80` 該行的註解；docker run 方式請加上 `-p 80:80`。
> 憑證儲存在 `/root/cert/<網域>/` 下，檔名固定為 `fullchain.pem` / `privkey.pem`，重新啟動後保留（上面 Docker 指令裡的掛載正是把這個路徑對應出來）。若網域/連接埠設定有誤，會自動回復 HTTP。
> ACME 僅支援 Linux，在 Windows 上會隱藏。

### 使用自備憑證

自己管理的憑證 —— Cloudflare 來源憑證、企業 CA、certbot 簽出的憑證 —— 可以在同一個
分頁裡註冊。2S-UI 會驗證檔案可讀、私鑰與憑證相符、以及憑證確實涵蓋該網域；接著這個
網域就能像其他網域一樣在「介面」與「訂閱」分頁中選用。已註冊的憑證會包含在資料庫
備份裡。

### 位於反向代理之後

開啟 **TLS 由反向代理終止** 開關，2S-UI 會替你寫好 vhost：
`/etc/nginx/conf.d/s-ui-proxy-<網域>.conf`，指向面板並帶上必要的轉送請求標頭，用
`nginx -t` 驗證、reload，任何一步失敗都會復原並原樣回傳 nginx 自己的錯誤訊息。訂閱
服務也可以放在同一個反向代理後方。

<details>
  <summary>想自己簽發憑證？（Certbot）</summary>

### Certbot

```bash
snap install core; snap refresh core
snap install --classic certbot
ln -s /snap/bin/certbot /usr/bin/certbot

certbot certonly --standalone --register-unsafely-without-email --non-interactive --agree-tos -d <Your Domain Name>
```

接著把簽出的 `fullchain.pem` / `privkey.pem` 註冊到 **網域與憑證** 分頁。

</details>

## Stargazers over Time
[![Star History Chart](https://api.star-history.com/svg?repos=shenaba/2s-ui&type=Date)](https://star-history.com/#shenaba/2s-ui&Date)
