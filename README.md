# <img src="frontend/public/assets/favicon.svg" width="44" height="44" align="texttop" alt=""> 2S-UI
[English](README.md) · [فارسی](README.fa.md) · [Tiếng Việt](README.vi.md) · [简体中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md) · [Русский](README.ru.md)

2S-UI is an open-source management panel for [sing-box](https://github.com/SagerNet/sing-box). It gives you a clean, multilingual interface for deploying, configuring and monitoring a wide range of proxy and VPN protocols — from a single VPS to a multi-node deployment.

2S-UI began as a fork of s-ui. The frontend is rewritten in full, with a range of features added on top to make the panel nicer to use.

![](https://img.shields.io/github/v/release/shenaba/2s-ui.svg)
[![Docker Pulls](https://img.shields.io/docker/pulls/shenaba/2s-ui.svg)](https://hub.docker.com/r/shenaba/2s-ui)
[![Go Report Card](https://goreportcard.com/badge/github.com/shenaba/2s-ui)](https://goreportcard.com/report/github.com/shenaba/2s-ui)
[![Downloads](https://img.shields.io/github/downloads/shenaba/2s-ui/total.svg)](https://github.com/shenaba/2s-ui/releases)
[![License](https://img.shields.io/badge/license-GPL%20V3-blue.svg?longCache=true)](https://www.gnu.org/licenses/gpl-3.0.en.html)

> **Disclaimer:** This project is only for personal learning and communication, please do not use it for illegal purposes, please do not use it in a production environment

## Features

- **Multi-protocol** — VLESS, VMess, Trojan, Shadowsocks, Hysteria2, TUIC,
  AnyTLS and more, inbound and outbound, plus WireGuard, WARP and Tailscale
  endpoints ([full list](#protocols)).
- **Central TLS** — Reality, uTLS fingerprints, XTLS, and certificates
  registered once then picked per inbound.
- **Routing rules** — matching on domain, IP, port, protocol, process, user and
  rule-set, combined with and/or, with a separate rule list for DNS.
- **Client management** — traffic quota, expiry date, IP limit, live online
  status, plus one-click share links, QR codes and subscriptions.
- **Traffic statistics** — per inbound, per client and per outbound, with reset
  controls.
- **Subscriptions** — `link`, `json` and `clash` formats, usage and expiry
  reported back to the client app, external links folded in.
- **Multi-node cluster** — node health monitoring, users shared across nodes,
  every node's servers merged into one subscription
  ([details](#multi-node-cluster)).
- **Automatic HTTPS** — Let's Encrypt issuance and renewal, plus an automatic
  nginx reverse proxy ([details](#domains--certificates)).
- **Notifications** — node, core, outbound, client, resource and login events
  pushed to Telegram, a webhook or e-mail, plus a Telegram bot that runs the
  panel from a phone ([details](#notifications--telegram-bot)).
- **Login protection** — persistent failure rate limiting, TOTP two-factor
  authentication, and every session retired on a credential change
  ([details](#login-protection)).
- **One-click updates** — in-place upgrades from the panel, checksum-verified.
- **Rebuilt interface** — a from-scratch frontend, hand-built components, dark
  and light themes, six languages including RTL.

<details id="protocols">
  <summary>Supported protocols</summary>

- General: Mixed, SOCKS, HTTP/HTTPS, Direct, Tun, Redirect, TProxy
- V2Ray based: VLESS, VMess, Trojan, Shadowsocks (incl. `plugin` / `plugin_opts`)
- Other protocols: ShadowTLS, Hysteria, Hysteria2, Naive¹, TUIC, AnyTLS, Snell²
- Inbound only: Cloudflared
- Outbound only: Tor, SSH, Bridge, Selector, URLTest
- Endpoints: WireGuard, WARP, Tailscale, OpenConnect, OpenVPN — with a latency test per endpoint or for all at once
- XTLS is supported, and Hysteria port hopping is available on the outbound form

<sup>1</sup> Naive needs the cronet toolchain, which does not build everywhere: official Linux
releases ship it on amd64, arm64, armv7 and 386 only. On armv6, armv5 and s390x a Naive
outbound reports that the binary was built without it.

<sup>2</sup> sing-box's Snell inbound speaks v5 and v6 while its outbound speaks v4 and v6, so
only a v6 listener gets a generated client config; a v5 listener is for clients configured by
hand (Surge).

</details>

## Languages

English · Farsi · Vietnamese · Chinese (Simplified) · Chinese (Traditional) · Russian

## Supported platforms

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

| | Default |
| --- | --- |
| Panel | port `2095`, path `/app/` |
| Subscription | port `2096`, path `/sub/` |
| User / password | `admin` / `admin` |

## Install

### Linux/macOS

```sh
bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
```

The language follows `$LANG`, or pass `en`, `fa`, `ru`, `vi`, `zhcn` or
`zhtw`:

```sh
SUI_LANG=zhcn bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
```

### Alpine Linux

Alpine ships neither `bash` nor `curl`. Add them first; the installer then
detects Alpine and sets the panel up as an OpenRC service:

```sh
apk add bash curl
bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
```

### Windows

1. Download the latest Windows release from [GitHub Releases](https://github.com/shenaba/2s-ui/releases/latest)
2. Extract the ZIP file
3. Run `install-windows.bat` as Administrator
4. Follow the installation wizard
5. Access the panel at http://localhost:2095/app

### Docker

```shell
mkdir 2s-ui && cd 2s-ui
wget -q https://raw.githubusercontent.com/shenaba/2s-ui/main/docker-compose.yml
docker compose up -d
```

<details>
  <summary>Without compose, or building your own image</summary>

If Docker itself is not installed yet:

```shell
curl -fsSL https://get.docker.com | sh
```

Plain `docker run`:

```shell
mkdir 2s-ui && cd 2s-ui
docker run -itd \
    -p 2095:2095 -p 2096:2096 -p 443:443 \
    -v $PWD/db/:/app/db/ \
    -v $PWD/cert/:/root/cert/ \
    --name s-ui --restart=unless-stopped \
    ghcr.io/shenaba/2s-ui:latest
```

Build your own image:

```shell
git clone https://github.com/shenaba/2s-ui
docker build -t 2s-ui .
```

</details>

<details>
  <summary>A specific version, manual installation, uninstall</summary>

**A specific version.** Add the version to the end of the installation command. e.g. `v1.5.5`:

```sh
VERSION=v1.5.5 && bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/$VERSION/install.sh) $VERSION
```

**Manual installation — Linux/macOS**

1. Get the latest version of 2S-UI based on your OS/Architecture from GitHub: [https://github.com/shenaba/2s-ui/releases/latest](https://github.com/shenaba/2s-ui/releases/latest)
2. **OPTIONAL** Get the latest version of `s-ui.sh` [https://raw.githubusercontent.com/shenaba/2s-ui/main/s-ui.sh](https://raw.githubusercontent.com/shenaba/2s-ui/main/s-ui.sh)
3. **OPTIONAL** Copy `s-ui.sh` to `/usr/bin/s-ui` and run `chmod +x /usr/bin/s-ui`.
4. Extract s-ui tar.gz file to a directory of your choice and navigate to the directory where you extracted the tar.gz file.
5. Copy *.service files to /etc/systemd/system/ and run `systemctl daemon-reload`.
6. Enable autostart and start 2S-UI service using `systemctl enable s-ui --now`
7. Start sing-box service using `systemctl enable sing-box --now`

**Manual installation — Windows**

1. Get the latest Windows version from GitHub: [https://github.com/shenaba/2s-ui/releases/latest](https://github.com/shenaba/2s-ui/releases/latest)
2. Download the appropriate Windows package (e.g., `s-ui-windows-amd64.zip`)
3. Extract the ZIP file to a directory of your choice
4. Run `install-windows.bat` as Administrator
5. Follow the installation wizard
6. Access the panel at http://localhost:2095/app

**Uninstall — systemd**

```sh
sudo -i

systemctl disable s-ui  --now

rm -f /etc/systemd/system/sing-box.service
systemctl daemon-reload

rm -fr /usr/local/s-ui
rm /usr/bin/s-ui
```

**Uninstall — OpenRC (Alpine)**

```sh
sudo -i

rc-service s-ui stop
rc-update del s-ui default
rm -f /etc/init.d/s-ui

rm -fr /usr/local/s-ui
rm /usr/bin/s-ui
```

</details>

### Upgrading

New releases are flagged on the version pill in the sidebar — the check is
client-side, so the panel host itself does not need to reach GitHub. On Linux
(systemd or Docker) one click upgrades in place: the panel downloads the
release, verifies it against the published `SHA256SUMS`, smoke-tests the new
binary, then replaces it and restarts. No SSH.

> A running `.exe` cannot replace itself, so on Windows the pill only links to
> the release page. In Docker the new binary lives in the container's writable
> layer: it survives `docker restart`, but recreating the container reverts to
> the image's version — pull a new image to make it stick.

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

<details>
  <summary>Driving node sync from the API</summary>

`POST <panel path>apiv2/save` (the panel path is `/app/` by default, so
`/app/apiv2/save`) triggers the web UI's immediate node fanout only when the
request carries `sync=true`; without it, client/inbound changes still converge
through the hourly reconcile safety net.

</details>

## Domains & Certificates

Everything TLS lives in the **Domains & Certificates** tab of Panel Settings.
Panel and subscription service each pick their own domain, and the certificate
paths follow the domain you select — no file paths to copy around.

**🔐 Automatic certificates (ACME / Let's Encrypt) — recommended.** Enter a
domain, add an email, and press issue: 2S-UI obtains and auto-renews a free
Let's Encrypt certificate, and the panel becomes reachable at
`https://<your-domain>:2095/app`. Requires TCP port **80** reachable from the
internet (HTTP-01 challenge). ACME is Linux-only and is hidden on Windows.

<details>
  <summary>How issuance works, and the Docker port-80 caveat</summary>

Issuance runs through **acme.sh**, which 2S-UI installs for you on first use
(along with `socat`, needed for standalone validation) and registers for
automatic renewal via acme.sh's own cron entry — there is nothing for you to
schedule.

The validation method defaults to **auto** — standalone when port 80 is free,
otherwise it borrows the running nginx, provisioning a minimal `server_name`
block under `/etc/nginx/conf.d` if one is missing. Pick **standalone** or
**nginx** explicitly if you would rather decide. Renewals hot-reload the
certificate; no restart needed.

> To publish port 80 with Docker: uncomment the `80:80` line in
> `docker-compose.yml`, or add `-p 80:80` to `docker run`. Certificates are
> stored under `/root/cert/<domain>/` as `fullchain.pem` / `privkey.pem` and
> survive restarts (the Docker volume above maps that path out). If the
> domain/port is misconfigured, 2S-UI falls back to HTTP.

</details>

<details>
  <summary>Bring your own certificate</summary>

Certificates you manage yourself — a Cloudflare origin CA, a corporate CA,
certbot output — can be registered in the same tab. 2S-UI verifies the files are
readable, that the key matches the certificate, and that the certificate really
covers the domain; the domain then becomes selectable on the Interface and
Subscription tabs like any other. Registered certificates are included in
database backups.

To issue one by hand with Certbot:

```bash
snap install core; snap refresh core
snap install --classic certbot
ln -s /snap/bin/certbot /usr/bin/certbot

certbot certonly --standalone --register-unsafely-without-email --non-interactive --agree-tos -d <Your Domain Name>
```

Then register the resulting `fullchain.pem` / `privkey.pem` under
**Domains & Certificates**.

</details>

<details>
  <summary>Behind a reverse proxy</summary>

Turn on **TLS terminated by a reverse proxy** and 2S-UI writes the vhost for
you: `/etc/nginx/conf.d/s-ui-proxy-<domain>.conf`, pointed at the panel with the
right forwarding headers, checked with `nginx -t`, reloaded, and rolled back
with nginx's own error message if anything fails. The subscription server can
sit behind the same proxy.

</details>

## Notifications & Telegram Bot

Everything lives in the **Notifications** tab of Panel Settings. Turn it on,
pick the events, and give it at least one channel:

- **Telegram** — a bot token and one or more chat IDs, optionally through a
  proxy or a self-hosted Bot API server.
- **Webhook** — every event delivered to a URL of your own.
- **E-mail** — plain SMTP.

Each channel has its own queue, so one that hangs cannot hold up the others.
The events: a node or the sing-box core going down or coming back, an outbound
failing its probe URL, a client running out of traffic or nearing expiry, CPU
or memory over a threshold, and logins — successful, failed and banned. The
thresholds, and how many failed probes make a node count as down, are settings
on the same tab; a condition that keeps repeating is reported once and then
not again for 24 hours. A scheduled report (cron or `@daily`) summarises the
panel, optionally with a database backup attached.

**Telegram bot**, on the same tab, turns those chat IDs into admin chats that
run the panel from a phone: `/status`, `/nodes`, `/clients` (those nearing a
limit, or `/clients <name>` to search), `/online`, `/traffic` for the last 24
hours, `/inbounds`, `/bans` and `/backup`. From a client card the operator can
hand out the subscription or a single inbound's link, as text and as a QR
code, and bind the client to a Telegram account by picking the contact. A
bound client can then ask the bot for their own usage and links, in their own
language, and is told directly when their subscription is about to run out —
a different message from the operator's, with no panel hostname on it. Anyone
else gets `/id` (their own Telegram ID, for the operator to bind) and nothing
that says which panel is behind the bot.

<img src="frontend/media/tgbot.jpg" width="360" alt="Telegram bot">

## Login Protection

Three things guard the login, all on by default:

- **Rate limiting** — five failures within five minutes lock an identity out
  for fifteen, counted against both the source IP and the username, so
  spreading attempts across either buys no fresh budget. The state is in the
  database, so a restart does not clear a lockout. The three numbers are on
  the **Interface** tab of Panel Settings; `0` in any of them disables it.
  Behind a reverse proxy the panel takes the client address from
  `X-Forwarded-For` only when its reverse-proxy mode is on — otherwise every
  attempt would be attributed to the proxy and share one budget.
- **Two-factor authentication** — TOTP, compatible with any authenticator
  app. Enable it from the shield icon on the **Admins** page. The secret is
  stored only once a code from it has been verified, so an abandoned enrolment
  cannot lock you out; if the authenticator itself is lost, `sui admin
  -disable-2fa` on the host turns it off.
- **Session invalidation** — changing a password or username retires every
  session issued before it, including open WebSocket connections.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, coding
conventions, testing, and the pull request process.

<details>
  <summary>Building and running from source</summary>

```shell
git clone https://github.com/shenaba/2s-ui
cd 2s-ui
./runSUI.sh
```

`build.sh` builds the frontend, copies it into `web/html/` for `//go:embed`,
and builds the binary with the required build tags; `runSUI.sh` runs it on top
of that. Building by hand needs those same tags — see
[CONTRIBUTING.md](CONTRIBUTING.md).

</details>

<details>
  <summary>Environment variables</summary>

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

## Special Thanks

- [@alireza0](https://github.com/alireza0)

## Stargazers over Time
[![Star History Chart](https://api.star-history.com/svg?repos=shenaba/2s-ui&type=Date)](https://star-history.com/#shenaba/2s-ui&Date)
