# <img src="frontend/public/assets/favicon.svg" width="44" height="44" align="texttop" alt=""> 2S-UI
[English](README.md) · [فارسی](README.fa.md) · [Tiếng Việt](README.vi.md) · [简体中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md) · [Русский](README.ru.md)

2S-UI 是一個開源的 [Sing-Box](https://github.com/SagerNet/sing-box) 管理面板，面向自架代理服務的部署與維運。協定設定、路由規則、使用者與訂閱、流量統計集中於同一套介面，支援六種語言與深淺雙主題；單機即可運行，多台亦可組成叢集統一管理。

2S-UI 由 [s-ui](https://github.com/alireza0/s-ui) 分支而來，重寫了整套前端，並引入多節點叢集、ACME 憑證自動簽發與續期、面板內一鍵升級，以及不中斷連線的使用者熱更新。

![](https://img.shields.io/github/v/release/shenaba/2s-ui.svg)
[![Docker Pulls](https://img.shields.io/docker/pulls/shenaba/2s-ui.svg)](https://hub.docker.com/r/shenaba/2s-ui)
[![Go Report Card](https://goreportcard.com/badge/github.com/shenaba/2s-ui)](https://goreportcard.com/report/github.com/shenaba/2s-ui)
[![Downloads](https://img.shields.io/github/downloads/shenaba/2s-ui/total.svg)](https://github.com/shenaba/2s-ui/releases)
[![License](https://img.shields.io/badge/license-GPL%20V3-blue.svg?longCache=true)](https://www.gnu.org/licenses/gpl-3.0.en.html)

> **免責聲明：** 本專案僅供個人學習與交流使用，請勿用於非法用途，請勿用於正式環境。

**如果你覺得這個專案有幫助，可以給一個** :star2:

## 功能

- **多協定** —— 入站出站支援 VLESS、VMess、Trojan、Shadowsocks、Hysteria2、TUIC、
  AnyTLS 等，另有 WireGuard / WARP / Tailscale 三類 Endpoint（[完整列表](#protocols)）
- **TLS 集中管理** —— Reality、uTLS 指紋、XTLS；憑證註冊一次，之後依入站選用
- **路由規則** —— 依網域、IP、連接埠、協定、行程、使用者或 rule-set 比對，可 and/or 組合。
  DNS 有自己獨立的一套規則。
- **多入站用戶端** —— 一個用戶端可掛在多個入站上，各帶流量上限與到期時間，超量或過期
  自動停用
- **配額自動化** —— 可以從首次使用才開始計時，也可以每 N 天自動重置，跑滿的用戶端到期
  自動恢復
- **每用戶端 IP 數限制** —— 限制單一用戶端同時使用的來源 IP 數量，超出的會被斷線並短暫
  拒絕重連，不依賴 fail2ban
- **使用者熱更新** —— 改用戶端時原地改寫入站的使用者表，不重建監聽器，其他人不會斷線
- **訂閱連結** —— 支援 `link`、`json`、`clash` 三種格式，回傳用量與到期時間，可併入外部
  連結
- **多節點叢集** —— 監控其他 2S-UI 面板、跨節點共用使用者、各節點線路合併進同一條訂閱
  （[詳見下文](#多節點叢集)）
- **HTTPS 自動化** —— Let's Encrypt 自動簽發續期，並自動產生 nginx 反向代理
  （[詳見下文](#網域與憑證)）
- **一鍵更新** —— 面板內原地升級，帶總和檢查碼驗證
- **即時儀表板** —— 系統資源、流量、協定佔比、網路吞吐、節點健康，每塊卡片都能單獨開關
- **帳號與語言** —— 支援多個面板管理員、帶到期時間的
  [API Token](https://github.com/shenaba/2s-ui/wiki/API-Documentation)、深色/淺色佈景主題、六種語言

<details id="protocols">
  <summary>支援的協定</summary>

- 通用協定：Mixed, SOCKS, HTTP/HTTPS, Direct, Tun, Redirect, TProxy
- V2Ray 系列：VLESS, VMess, Trojan, Shadowsocks（支援 `plugin` / `plugin_opts`）
- 其他協定：ShadowTLS, Hysteria, Hysteria2, Naive¹, TUIC, AnyTLS
- 僅出站：Tor, SSH, Selector, URLTest
- Endpoints：WireGuard、WARP、Tailscale——可單獨測延遲，也可一鍵測全部
- 支援 XTLS 協定，出站表單支援 Hysteria 連接埠跳躍

<sup>1</sup> Naive 依賴 cronet 工具鏈，並非所有平台都能編譯：官方 Linux 發佈版只在
amd64、arm64、armv7 與 386 上帶這個協定。在 armv6、armv5、s390x 上使用 Naive 出站會提示
該執行檔未編譯此協定。

</details>

<details>
  <summary>語言</summary>

English · Farsi · Vietnamese · Chinese (Simplified) · Chinese (Traditional) · Russian

</details>

<details>
  <summary>螢幕截圖</summary>

!["Main"](frontend/media/main.png)

更多螢幕截圖：[frontend/screenshots.md](frontend/screenshots.md)

</details>

## 安裝

### Linux/macOS

```sh
bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
```

裝好後開啟 `http://<你的伺服器>:2095/app/`，以 `admin` / `admin` 登入。

| | 預設值 |
| --- | --- |
| 面板 | 連接埠 `2095`，路徑 `/app/` |
| 訂閱 | 連接埠 `2096`，路徑 `/sub/` |
| 使用者名稱 / 密碼 | `admin` / `admin` |

同時支援 systemd 與 OpenRC（Alpine），安裝腳本會自動選擇。腳本支援與面板相同的六種
語言：`en`、`fa`、`ru`、`vi`、`zhcn`、`zhtw`，預設跟隨系統 `$LANG`，也可以手動指定，
之後 `s-ui` 選單會沿用該語言：

```sh
SUI_LANG=zhtw bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
```

### Windows

1. 從 [GitHub Releases](https://github.com/shenaba/2s-ui/releases/latest) 下載最新 Windows 版本
2. 解壓縮 ZIP 檔案
3. 以系統管理員身分執行 `install-windows.bat`
4. 依安裝精靈操作
5. 透過 http://localhost:2095/app 存取面板

### Docker

```shell
mkdir 2s-ui && cd 2s-ui
wget -q https://raw.githubusercontent.com/shenaba/2s-ui/main/docker-compose.yml
docker compose up -d
```

<details>
  <summary>不使用 compose，或自行建置映像檔</summary>

如果尚未安裝 Docker：

```shell
curl -fsSL https://get.docker.com | sh
```

直接使用 `docker run`：

```shell
mkdir 2s-ui && cd 2s-ui
docker run -itd \
    -p 2095:2095 -p 2096:2096 -p 443:443 \
    -v $PWD/db/:/app/db/ \
    -v $PWD/cert/:/root/cert/ \
    --name s-ui --restart=unless-stopped \
    ghcr.io/shenaba/2s-ui:latest
```

自行建置映像檔：

```shell
git clone https://github.com/shenaba/2s-ui
docker build -t 2s-ui .
```

</details>

<details>
  <summary>安裝歷史版本、手動安裝、解除安裝</summary>

**安裝歷史版本。** 在安裝指令末尾加上版本號，例如 `v1.5.5`：

```sh
VERSION=v1.5.5 && bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/$VERSION/install.sh) $VERSION
```

**手動安裝 —— Linux/macOS**

1. 根據你的系統與架構，從 GitHub 下載最新版本：[https://github.com/shenaba/2s-ui/releases/latest](https://github.com/shenaba/2s-ui/releases/latest)
2. **可選：** 取得最新的 `s-ui.sh`：[https://raw.githubusercontent.com/shenaba/2s-ui/main/s-ui.sh](https://raw.githubusercontent.com/shenaba/2s-ui/main/s-ui.sh)
3. **可選：** 將 `s-ui.sh` 複製到 `/usr/bin/s-ui`，並執行 `chmod +x /usr/bin/s-ui`
4. 將 `s-ui` 的 tar.gz 檔案解壓縮到你選擇的目錄，並進入解壓縮後的目錄
5. 將 `*.service` 檔案複製到 `/etc/systemd/system/`，並執行 `systemctl daemon-reload`
6. 啟用開機自動啟動並啟動 2S-UI 服務：`systemctl enable s-ui --now`
7. 啟動 sing-box 服務：`systemctl enable sing-box --now`

**手動安裝 —— Windows**

1. 從 GitHub 取得最新 Windows 版本：[https://github.com/shenaba/2s-ui/releases/latest](https://github.com/shenaba/2s-ui/releases/latest)
2. 下載對應的 Windows 安裝包，例如 `s-ui-windows-amd64.zip`
3. 將 ZIP 檔案解壓縮到你選擇的目錄
4. 以系統管理員身分執行 `install-windows.bat`
5. 依安裝精靈操作
6. 透過 http://localhost:2095/app 存取面板

**解除安裝**

```sh
sudo -i

systemctl disable s-ui  --now

rm -f /etc/systemd/system/sing-box.service
systemctl daemon-reload

rm -fr /usr/local/s-ui
rm /usr/bin/s-ui
```

</details>

### 升級

有新版本會在側邊欄的版本標籤上提示 —— 檢查在用戶端完成，所以面板所在的伺服器連不上
GitHub 也會提示。Linux 上（systemd 或 Docker）點一下即可原地升級：面板會下載新版本，
用官方發佈的 `SHA256SUMS` 校驗，先試跑一次新的執行檔，再替換並重新啟動。不必 SSH。

> Windows 上執行中的 `.exe` 無法自我替換，版本標籤只會連到 release 頁面。
> Docker 裡新的執行檔寫在容器可寫層：`docker restart` 後仍在，但重建容器會退回映像
> 檔自帶的版本——想長期生效請拉取新映像檔。

### 支援平台

| 平台 | 架構 | 狀態 |
| ---- | ---- | ---- |
| Linux | amd64, arm64, armv7, armv6, armv5, 386, s390x | 支援 |
| Windows | amd64, 386, arm64 | 支援 |
| macOS | amd64, arm64 | 實驗性 |

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

<details>
  <summary>以 API 驅動節點同步</summary>

`POST <面板路徑>apiv2/save`（面板路徑預設為 `/app/`，即 `/app/apiv2/save`）只有在請求
帶上 `sync=true` 時，才會觸發 Web UI 那種立即下發到各節點的行為；未帶該參數時，用戶端
與入站的變更仍會透過每小時一次的對帳安全網收斂。

</details>

## 網域與憑證

TLS 相關的設定都集中在面板設定的 **網域與憑證** 分頁。面板與訂閱服務各自選擇自己的
網域，憑證路徑會跟著所選網域自動決定，不必再手動填檔案路徑。

**🔐 網域自動申請憑證（ACME / Let's Encrypt）—— 推薦。** 填寫網域、填上電子郵件、按下
簽發：2S-UI 即自動簽發並自動續期免費的 Let's Encrypt 憑證，接著即可透過
`https://<你的網域>:2095/app` 存取面板。需要 TCP **80** 連接埠可從公網存取（HTTP-01
校驗）。ACME 僅支援 Linux，在 Windows 上會隱藏。

<details>
  <summary>簽發的實際過程，以及 Docker 的 80 連接埠注意事項</summary>

簽發底層走 **acme.sh** —— 首次使用時面板會自動幫你安裝 acme.sh（以及 standalone 驗證
需要的 `socat`），並啟用 acme.sh 自帶的 cron 做自動續期，你不需要自己設定任何排程工作。

驗證方式預設為 **auto** —— 80 連接埠閒置時用 standalone，否則借用正在執行的 nginx，
必要時會在 `/etc/nginx/conf.d` 下自動補一個最小的 `server_name` 設定區塊。你也可以
明確指定 **standalone** 或 **nginx**。續期時憑證會熱重載，不需重新啟動。

> Docker 部署對應 80 連接埠：docker compose 方式請取消 `docker-compose.yml` 中 `80:80`
> 該行的註解；docker run 方式請加上 `-p 80:80`。憑證儲存在 `/root/cert/<網域>/` 下，
> 檔名固定為 `fullchain.pem` / `privkey.pem`，重新啟動後保留（上面 Docker 指令裡的掛載
> 正是把這個路徑對應出來）。若網域/連接埠設定有誤，會自動回復 HTTP。

</details>

<details>
  <summary>使用自備憑證</summary>

自己管理的憑證 —— Cloudflare 來源憑證、企業 CA、certbot 簽出的憑證 —— 可以在同一個
分頁裡註冊。2S-UI 會驗證檔案可讀、私鑰與憑證相符、以及憑證確實涵蓋該網域；接著這個
網域就能像其他網域一樣在「介面」與「訂閱」分頁中選用。已註冊的憑證會包含在資料庫
備份裡。

想用 Certbot 自己簽發：

```bash
snap install core; snap refresh core
snap install --classic certbot
ln -s /snap/bin/certbot /usr/bin/certbot

certbot certonly --standalone --register-unsafely-without-email --non-interactive --agree-tos -d <Your Domain Name>
```

接著把簽出的 `fullchain.pem` / `privkey.pem` 註冊到 **網域與憑證** 分頁。

</details>

<details>
  <summary>位於反向代理之後</summary>

開啟 **TLS 由反向代理終止** 開關，2S-UI 會替你寫好 vhost：
`/etc/nginx/conf.d/s-ui-proxy-<網域>.conf`，指向面板並帶上必要的轉送請求標頭，用
`nginx -t` 驗證、reload，任何一步失敗都會復原並原樣回傳 nginx 自己的錯誤訊息。訂閱
服務也可以放在同一個反向代理後方。

</details>

## 參與貢獻

請查看 [CONTRIBUTING.md](CONTRIBUTING.md)，瞭解開發環境、程式碼規範、測試與 Pull Request 流程。

<details>
  <summary>從原始碼建置並執行</summary>

```shell
git clone https://github.com/shenaba/2s-ui
cd 2s-ui
./runSUI.sh
```

`build.sh` 會建置前端、把產物複製進 `web/html/` 供 `//go:embed` 使用，再帶上必要的
build tags 建置執行檔；`runSUI.sh` 在此之上直接把它跑起來。手動建置同樣要帶上那些
tags，詳見 [CONTRIBUTING.md](CONTRIBUTING.md)。

</details>

<details>
  <summary>環境變數</summary>

| 變數 | 類型 | 預設值 |
| ---- | :--: | :---- |
| SUI_LOG_LEVEL | `"debug"` \| `"info"` \| `"warn"` \| `"error"` | `"info"` |
| SUI_DEBUG | `boolean` | `false` |
| SUI_DB_FOLDER | `string` | `"db"` |
| SUI_BIN_FOLDER | `string` | `"bin"` |

`SUI_BIN_FOLDER` 僅在從舊的子行程版本遷移資料庫時讀取；sing-box 已內嵌進執行檔，
執行時不存在 `bin/` 目錄。

</details>

## 特別感謝

- [@alireza0](https://github.com/alireza0)

## Stargazers over Time
[![Star History Chart](https://api.star-history.com/svg?repos=shenaba/2s-ui&type=Date)](https://star-history.com/#shenaba/2s-ui&Date)
