# <img src="frontend/public/assets/favicon.svg" width="44" height="44" align="texttop" alt=""> 2S-UI
[English](README.md) · [فارسی](README.fa.md) · [Tiếng Việt](README.vi.md) · [简体中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md) · [Русский](README.ru.md)

**An actively maintained sing-box web panel for multi-protocol proxy management, subscription delivery, traffic monitoring, and self-hosted deployment.**

![](https://img.shields.io/github/v/release/shenaba/2s-ui.svg)
[![Docker Pulls](https://img.shields.io/docker/pulls/shenaba/2s-ui.svg)](https://hub.docker.com/r/shenaba/2s-ui)
[![Go Report Card](https://goreportcard.com/badge/github.com/shenaba/2s-ui)](https://goreportcard.com/report/github.com/shenaba/2s-ui)
[![Downloads](https://img.shields.io/github/downloads/shenaba/2s-ui/total.svg)](https://github.com/shenaba/2s-ui/releases)
[![License](https://img.shields.io/badge/license-GPL%20V3-blue.svg?longCache=true)](https://www.gnu.org/licenses/gpl-3.0.en.html)

> **Disclaimer:** This project is only for personal learning and communication, please do not use it for illegal purposes, please do not use it in a production environment

**If you think this project is helpful to you, you may wish to give a**:star2:

**Want to contribute?** See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, coding conventions, testing, and the pull request process.

2S-UI is based on [alireza0/s-ui](https://github.com/alireza0/s-ui) and is maintained as a continued fork. It keeps the original panel direction while updating sing-box support, multi-protocol capabilities, deployment scripts, and ongoing fixes. Thanks to the original author and contributors.

## Quick Overview
| Features                               |      Enable?       |
| -------------------------------------- | :----------------: |
| Multi-Protocol                         | :heavy_check_mark: |
| Multi-Language                         | :heavy_check_mark: |
| Multi-Client/Inbound                   | :heavy_check_mark: |
| Advanced Traffic Routing Interface     | :heavy_check_mark: |
| Client & Traffic & System Status       | :heavy_check_mark: |
| Subscription Link (link/json/clash + info)| :heavy_check_mark: |
| **Multi-Node Cluster (shared users across servers)** ✨ | :heavy_check_mark: |
| **Automatic HTTPS (ACME / Let's Encrypt)** ✨ | :heavy_check_mark: |
| **Automatic nginx Reverse Proxy** ✨    | :heavy_check_mark: |
| **In-Panel Self-Update** ✨             | :heavy_check_mark: |
| Dark/Light Theme                       | :heavy_check_mark: |
| API Interface                          | :heavy_check_mark: |

## Supported Platforms
| Platform | Architecture | Status |
|----------|--------------|---------|
| Linux    | amd64, arm64, armv7, armv6, armv5, 386, s390x | ✅ Supported |
| Windows  | amd64, 386, arm64 | ✅ Supported |
| macOS    | amd64, arm64 | 🚧 Experimental |

## Screenshots

!["Main"](frontend/media/main.png)

[Other UI Screenshots](frontend/screenshots.md)

## API Documentation

[API-Documentation Wiki](https://github.com/shenaba/2s-ui/wiki/API-Documentation)

## Default Installation Information
- Panel Port: 2095
- Panel Path: /app/
- Subscription Port: 2096
- Subscription Path: /sub/
- User/Password: admin

## Install & Upgrade to Latest Version

### From the panel (upgrades only)

Your browser checks GitHub for new releases on every page load and flags one on
the version pill in the sidebar — that check is client-side, so the panel host
itself does not need to reach GitHub for the chip to appear. Installing is
server-side: on Linux — bare metal under systemd, or Docker — one click has the
panel download the release, verify it against the published `SHA256SUMS`,
smoke-test the new binary, then replace it in place and restart. No SSH, no
install script.

> On Windows a running `.exe` cannot replace itself, so the pill only links to
> the release page. In Docker the new binary lives in the container's writable
> layer: it survives `docker restart`, but recreating the container reverts to
> the image's version — pull a new image to make it stick.

### Linux/macOS
```sh
bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
```

systemd and OpenRC (Alpine) are both supported; the installer picks the right
one for your system.

The installer speaks the same six languages as the panel — `en`, `fa`, `ru`,
`vi`, `zhcn`, `zhtw`. It follows your system `$LANG`, or you can pick one, which
the `s-ui` menu then remembers:

```sh
SUI_LANG=zhcn bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
```

### Windows
1. Download the latest Windows release from [GitHub Releases](https://github.com/shenaba/2s-ui/releases/latest)
2. Extract the ZIP file
3. Run `install-windows.bat` as Administrator
4. Follow the installation wizard

## Install legacy Version

**Step 1:** To install your desired legacy version, add the version to the end of the installation command. e.g., ver `v1.5.5`:

```sh
VERSION=v1.5.5 && bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/$VERSION/install.sh) $VERSION
```

## Manual installation

### Linux/macOS
1. Get the latest version of 2S-UI based on your OS/Architecture from GitHub: [https://github.com/shenaba/2s-ui/releases/latest](https://github.com/shenaba/2s-ui/releases/latest)
2. **OPTIONAL** Get the latest version of `s-ui.sh` [https://raw.githubusercontent.com/shenaba/2s-ui/main/s-ui.sh](https://raw.githubusercontent.com/shenaba/2s-ui/main/s-ui.sh)
3. **OPTIONAL** Copy `s-ui.sh` to `/usr/bin/s-ui` and run `chmod +x /usr/bin/s-ui`.
4. Extract s-ui tar.gz file to a directory of your choice and navigate to the directory where you extracted the tar.gz file.
5. Copy *.service files to /etc/systemd/system/ and run `systemctl daemon-reload`.
6. Enable autostart and start 2S-UI service using `systemctl enable s-ui --now`
7. Start sing-box service using `systemctl enable sing-box --now`

### Windows
1. Get the latest Windows version from GitHub: [https://github.com/shenaba/2s-ui/releases/latest](https://github.com/shenaba/2s-ui/releases/latest)
2. Download the appropriate Windows package (e.g., `s-ui-windows-amd64.zip`)
3. Extract the ZIP file to a directory of your choice
4. Run `install-windows.bat` as Administrator
5. Follow the installation wizard
6. Access the panel at http://localhost:2095/app

## Uninstall 2S-UI

```sh
sudo -i

systemctl disable s-ui  --now

rm -f /etc/systemd/system/sing-box.service
systemctl daemon-reload

rm -fr /usr/local/s-ui
rm /usr/bin/s-ui
```

## Install using Docker

<details>
   <summary>Click for details</summary>

### Usage

**Step 1:** Install Docker

```shell
curl -fsSL https://get.docker.com | sh
```

**Step 2:** Install 2S-UI

> Docker compose method

```shell
mkdir 2s-ui && cd 2s-ui
wget -q https://raw.githubusercontent.com/shenaba/2s-ui/main/docker-compose.yml
docker compose up -d
```

> Use docker

```shell
mkdir 2s-ui && cd 2s-ui
docker run -itd \
    -p 2095:2095 -p 2096:2096 -p 443:443 \
    -v $PWD/db/:/app/db/ \
    -v $PWD/cert/:/root/cert/ \
    --name s-ui --restart=unless-stopped \
    ghcr.io/shenaba/2s-ui:latest
```

> Build your own image

```shell
git clone https://github.com/shenaba/2s-ui
docker build -t 2s-ui .
```

</details>

## Manual run ( contribution )

<details>
   <summary>Click for details</summary>

### Build and run whole project
```shell
./runSUI.sh
```

### Clone the repository
```shell
# clone repository
git clone https://github.com/shenaba/2s-ui
```


### - Frontend

The frontend code lives in the [`frontend/`](frontend) directory of this repository.

### - Backend
> Please build frontend once before!

To build backend:
```shell
# remove old frontend compiled files
rm -fr web/html/*
# apply new frontend compiled files
cp -R frontend/dist/ web/html/
# build
go build -o sui main.go
```

To run backend (from root folder of repository):
```shell
./sui
```

</details>

## Languages

- English
- Farsi
- Vietnamese
- Chinese (Simplified)
- Chinese (Traditional)
- Russian

## Features

- Supported protocols:
  - General: Mixed, SOCKS, HTTP/HTTPS, Direct, Tun, Redirect, TProxy
  - V2Ray based: VLESS, VMess, Trojan, Shadowsocks (incl. `plugin` / `plugin_opts`)
  - Other protocols: ShadowTLS, Hysteria, Hysteria2, Naive¹, TUIC, AnyTLS
  - Outbound only: Tor, SSH, Selector, URLTest
  - Endpoints: WireGuard, WARP, Tailscale — with a latency test per endpoint or for all at once

  <sup>1</sup> Naive needs the cronet toolchain, which does not build everywhere: official Linux
  releases ship it on amd64, arm64, armv7 and 386 only. On armv6, armv5 and s390x a Naive
  outbound reports that the binary was built without it.

- Supports XTLS protocols
- An advanced interface for routing traffic, incorporating PROXY Protocol, External, and Transparent Proxy, SSL Certificate, and Port
- An advanced interface for inbound and outbound configuration
- Clients’ traffic cap and expiration date; enable or disable a client straight from the list
- **Live user updates** — on VLESS, VMess, Trojan, Shadowsocks, AnyTLS, Hysteria, Hysteria2 and TUIC, adding, editing or removing a client updates the inbound's user table in place instead of rebuilding the listener, so everyone else keeps their connections — which matters most on the QUIC-based protocols, where a restart drops every session. Other inbound types still restart
- Hysteria port hopping on the outbound form
- Displays online clients, inbounds and outbounds with traffic statistics, and system status monitoring
- Subscription service with ability to add external links and subscription
- **Multi-node cluster** — monitor other 2S-UI panels, share users across them, and merge their servers into one subscription (see below)
- HTTPS for secure access to the web panel and subscription service (self-provided domain + SSL certificate)
- **Automatic SSL certificates** — just enter a domain and 2S-UI issues and auto-renews a free Let's Encrypt certificate for you (acme.sh is installed and driven for you; nothing to schedule)
- **Automatic nginx reverse proxy** — 2S-UI writes and validates the vhost when you put the panel behind a proxy
- **In-panel self-update** over checksum-verified GitHub releases
- Dark/Light theme

## Multi-Node Cluster

One panel can manage the others. Add a remote 2S-UI instance on the **Nodes**
page with its address and an API token, and the master will:

- **Monitor it** — a 5-second heartbeat reports each node as online, offline, or
  core-stopped (panel reachable but sing-box down).
- **Share users with it** — clients on the master that reference a node's
  inbounds are pushed to that node and kept in sync, with each node's traffic
  folded back into the master's counters. Sync is scoped to a `@cluster` group,
  so a node's own local users are never touched.
- **Fold its servers into one subscription** — a client's subscription link
  carries the master's servers and every bound node's servers together.

A node is just another 2S-UI instance talking over the v2 API (`Token` header):
no agent to install, and the only node-side setup is creating that API token in
its own panel, so existing panels can be adopted as they are. Inbounds adopted
from a node become read-only replicas on the master — edit them on the node they
belong to.

For API automation: `POST <panel path>apiv2/save` (the panel path is `/app/` by
default, so `/app/apiv2/save`) triggers the web UI's immediate node fanout only
when the request carries `sync=true`; without it, client/inbound changes still
converge through the hourly reconcile safety net.

## Environment Variables

<details>
  <summary>Click for details</summary>

### Usage

| Variable       |                      Type                      | Default       |
| -------------- | :--------------------------------------------: | :------------ |
| SUI_LOG_LEVEL  | `"debug"` \| `"info"` \| `"warn"` \| `"error"` | `"info"`      |
| SUI_DEBUG      |                   `boolean`                    | `false`       |
| SUI_DB_FOLDER  |                    `string`                    | `"db"`        |
| SUI_BIN_FOLDER |                    `string`                    | `"bin"`       |

`SUI_BIN_FOLDER` is only read while migrating a database from the old
subprocess-based layout; sing-box is embedded in the binary and there is no
`bin/` folder at runtime.

</details>

## Domains & Certificates

Everything TLS lives in the **Domains & Certificates** tab of Panel Settings.
Panel and subscription service each pick their own domain, and the certificate
paths follow the domain you select — no file paths to copy around.

### 🔐 Automatic Certificates (ACME / Let's Encrypt) — Recommended

Enter a domain, add an email, and press issue: 2S-UI obtains and auto-renews a
free Let's Encrypt certificate. Issuance runs through **acme.sh**, which 2S-UI
installs for you on first use (along with `socat`, needed for standalone
validation) and registers for automatic renewal via acme.sh's own cron entry —
there is nothing for you to schedule. Once done, the panel is reachable at
`https://<your-domain>:2095/app`.

The validation method defaults to **auto** — standalone when port 80 is free,
otherwise it borrows the running nginx, provisioning a minimal `server_name`
block under `/etc/nginx/conf.d` if one is missing. Pick **standalone** or
**nginx** explicitly if you would rather decide. Renewals hot-reload the
certificate; no restart needed.

> Requires TCP port **80** reachable from the internet (HTTP-01 challenge). To
> publish port 80 with Docker: uncomment the `80:80` line in `docker-compose.yml`,
> or add `-p 80:80` to `docker run`. Certificates are stored under
> `/root/cert/<domain>/` as `fullchain.pem` / `privkey.pem` and survive restarts
> (the Docker volume above maps that path out). If the domain/port is
> misconfigured, 2S-UI falls back to HTTP.
> ACME is Linux-only and is hidden on Windows.

### Bring your own certificate

Certificates you manage yourself — a Cloudflare origin CA, a corporate CA,
certbot output — can be registered in the same tab. 2S-UI verifies the files are
readable, that the key matches the certificate, and that the certificate really
covers the domain; the domain then becomes selectable on the Interface and
Subscription tabs like any other. Registered certificates are included in
database backups.

### Behind a reverse proxy

Turn on **TLS terminated by a reverse proxy** and 2S-UI writes the vhost for
you: `/etc/nginx/conf.d/s-ui-proxy-<domain>.conf`, pointed at the panel with the
right forwarding headers, checked with `nginx -t`, reloaded, and rolled back
with nginx's own error message if anything fails. The subscription server can
sit behind the same proxy.

<details>
  <summary>Prefer to issue certificates by hand? (Certbot)</summary>

### Certbot

```bash
snap install core; snap refresh core
snap install --classic certbot
ln -s /snap/bin/certbot /usr/bin/certbot

certbot certonly --standalone --register-unsafely-without-email --non-interactive --agree-tos -d <Your Domain Name>
```

Then register the resulting `fullchain.pem` / `privkey.pem` under
**Domains & Certificates**.

</details>

## Stargazers over Time
[![Star History Chart](https://api.star-history.com/svg?repos=shenaba/2s-ui&type=Date)](https://star-history.com/#shenaba/2s-ui&Date)
