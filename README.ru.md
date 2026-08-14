# <img src="frontend/public/assets/favicon.svg" width="44" height="44" align="texttop" alt=""> 2S-UI
[English](README.md) · [فارسی](README.fa.md) · [Tiếng Việt](README.vi.md) · [简体中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md) · [Русский](README.ru.md)

2S-UI — открытая панель управления [sing-box](https://github.com/SagerNet/sing-box). Она даёт простой многоязычный интерфейс для развёртывания, настройки и мониторинга самых разных прокси- и VPN-протоколов — от одного VPS до конфигурации из нескольких узлов.

2S-UI вырос из форка s-ui: фронтенд переписан целиком, а сверху добавлено множество возможностей, делающих работу с панелью удобнее.

![](https://img.shields.io/github/v/release/shenaba/2s-ui.svg)
[![Docker Pulls](https://img.shields.io/docker/pulls/shenaba/2s-ui.svg)](https://hub.docker.com/r/shenaba/2s-ui)
[![Go Report Card](https://goreportcard.com/badge/github.com/shenaba/2s-ui)](https://goreportcard.com/report/github.com/shenaba/2s-ui)
[![Downloads](https://img.shields.io/github/downloads/shenaba/2s-ui/total.svg)](https://github.com/shenaba/2s-ui/releases)
[![License](https://img.shields.io/badge/license-GPL%20V3-blue.svg?longCache=true)](https://www.gnu.org/licenses/gpl-3.0.en.html)

> **Отказ от ответственности:** Этот проект предназначен только для личного обучения и общения. Пожалуйста, не используйте его в незаконных целях и не используйте его в производственной среде.

## Возможности

- **Мультипротокольность** — VLESS, VMess, Trojan, Shadowsocks, Hysteria2, TUIC,
  AnyTLS и другие, на входящих и исходящих, плюс endpoints WireGuard, WARP и
  Tailscale ([полный список](#protocols)).
- **TLS в одном месте** — Reality, отпечатки uTLS, XTLS и сертификаты, которые
  регистрируются один раз, а затем выбираются для каждого входящего.
- **Правила маршрутизации** — совпадение по домену, IP, порту, протоколу,
  процессу, пользователю и rule-set, с объединением через and/or; у DNS свой
  отдельный список.
- **Управление клиентами** — квота трафика, срок действия, лимит IP, статус
  «онлайн» в реальном времени, а также ссылки для обмена, QR-коды и подписки в
  одно нажатие.
- **Статистика трафика** — по входящим, по клиентам и по исходящим, с
  управлением сбросом.
- **Подписки** — форматы `link`, `json` и `clash`, расход и срок действия
  возвращаются в клиентское приложение, внешние ссылки включаются в общую выдачу.
- **Кластер из нескольких узлов** — мониторинг состояния узлов, общие
  пользователи между ними и объединение их серверов в одну подписку
  ([подробнее](#кластер-из-нескольких-узлов)).
- **Автоматический HTTPS** — выпуск и продление сертификатов Let's Encrypt, плюс
  автоматический обратный прокси nginx ([подробнее](#домены-и-сертификаты)).
- **Обновление в одно нажатие** — прямо из панели, с проверкой контрольных сумм.
- **Переписанный интерфейс** — фронтенд с нуля, собственные компоненты, тёмная и
  светлая темы, шесть языков (включая RTL).

<details id="protocols">
  <summary>Поддерживаемые протоколы</summary>

- Общие: Mixed, SOCKS, HTTP/HTTPS, Direct, Tun, Redirect, TProxy
- На основе V2Ray: VLESS, VMess, Trojan, Shadowsocks (в т. ч. `plugin` / `plugin_opts`)
- Другие протоколы: ShadowTLS, Hysteria, Hysteria2, Naive¹, TUIC, AnyTLS
- Только исходящие: Tor, SSH, Selector, URLTest
- Endpoints: WireGuard, WARP, Tailscale — с проверкой задержки для отдельного endpoint или для всех сразу
- Поддерживается XTLS, а в форме исходящего доступен Hysteria port hopping

<sup>1</sup> Naive требует тулчейн cronet, который собирается не везде: официальные сборки для
Linux включают его только на amd64, arm64, armv7 и 386. На armv6, armv5 и s390x исходящее
соединение Naive сообщит, что бинарник собран без него.

</details>

## Языки

Английский · Фарси · Вьетнамский · Китайский (упрощённый) · Китайский (традиционный) · Русский

## Скриншоты

!["Main"](frontend/media/main.png)

[Другие скриншоты интерфейса](frontend/screenshots.md)

## Документация API

[Wiki с документацией API](https://github.com/shenaba/2s-ui/wiki/API-Documentation)

## Параметры установки по умолчанию

| | По умолчанию |
| --- | --- |
| Панель | порт `2095`, путь `/app/` |
| Подписка | порт `2096`, путь `/sub/` |
| Пользователь / пароль | `admin` / `admin` |

## Установка

### Linux/macOS

```sh
bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
```

Язык следует `$LANG`, либо укажите `en`, `fa`, `ru`, `vi`, `zhcn` или
`zhtw`:

```sh
SUI_LANG=ru bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
```

### Alpine Linux

В Alpine нет ни `bash`, ни `curl` — доставьте их, после чего установщик сам
определит Alpine и поставит панель как службу OpenRC:

```sh
apk add bash curl
bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
```

### Windows

1. Скачайте последний релиз для Windows с [GitHub Releases](https://github.com/shenaba/2s-ui/releases/latest)
2. Распакуйте ZIP-файл
3. Запустите `install-windows.bat` от имени администратора
4. Следуйте указаниям мастера установки
5. Откройте панель по адресу http://localhost:2095/app

### Docker

```shell
mkdir 2s-ui && cd 2s-ui
wget -q https://raw.githubusercontent.com/shenaba/2s-ui/main/docker-compose.yml
docker compose up -d
```

<details>
  <summary>Без compose или сборка собственного образа</summary>

Если Docker ещё не установлен:

```shell
curl -fsSL https://get.docker.com | sh
```

Обычный `docker run`:

```shell
mkdir 2s-ui && cd 2s-ui
docker run -itd \
    -p 2095:2095 -p 2096:2096 -p 443:443 \
    -v $PWD/db/:/app/db/ \
    -v $PWD/cert/:/root/cert/ \
    --name s-ui --restart=unless-stopped \
    ghcr.io/shenaba/2s-ui:latest
```

Сборка собственного образа:

```shell
git clone https://github.com/shenaba/2s-ui
docker build -t 2s-ui .
```

</details>

<details>
  <summary>Конкретная версия, ручная установка, удаление</summary>

**Конкретная версия.** Добавьте версию в конец команды установки. Например, `v1.5.5`:

```sh
VERSION=v1.5.5 && bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/$VERSION/install.sh) $VERSION
```

**Ручная установка — Linux/macOS**

1. Получите последнюю версию 2S-UI для вашей ОС/архитектуры с GitHub: [https://github.com/shenaba/2s-ui/releases/latest](https://github.com/shenaba/2s-ui/releases/latest)
2. **НЕОБЯЗАТЕЛЬНО** Получите последнюю версию `s-ui.sh` [https://raw.githubusercontent.com/shenaba/2s-ui/main/s-ui.sh](https://raw.githubusercontent.com/shenaba/2s-ui/main/s-ui.sh)
3. **НЕОБЯЗАТЕЛЬНО** Скопируйте `s-ui.sh` в `/usr/bin/s-ui` и выполните `chmod +x /usr/bin/s-ui`.
4. Распакуйте файл s-ui tar.gz в выбранный каталог и перейдите в каталог, куда вы распаковали файл tar.gz.
5. Скопируйте файлы *.service в /etc/systemd/system/ и выполните `systemctl daemon-reload`.
6. Включите автозапуск и запустите службу 2S-UI с помощью `systemctl enable s-ui --now`
7. Запустите службу sing-box с помощью `systemctl enable sing-box --now`

**Ручная установка — Windows**

1. Получите последнюю версию для Windows с GitHub: [https://github.com/shenaba/2s-ui/releases/latest](https://github.com/shenaba/2s-ui/releases/latest)
2. Скачайте подходящий пакет для Windows (например, `s-ui-windows-amd64.zip`)
3. Распакуйте ZIP-файл в выбранный каталог
4. Запустите `install-windows.bat` от имени администратора
5. Следуйте указаниям мастера установки
6. Откройте панель по адресу http://localhost:2095/app

**Удаление**

```sh
sudo -i

systemctl disable s-ui  --now

rm -f /etc/systemd/system/sing-box.service
systemctl daemon-reload

rm -fr /usr/local/s-ui
rm /usr/bin/s-ui
```

</details>

### Обновление

Новые релизы отмечаются на плашке версии в боковой панели — проверку выполняет
браузер, поэтому самому серверу с панелью доступ к GitHub не нужен. На Linux
(systemd или Docker) обновление выполняется в одно нажатие: панель скачивает
релиз, сверяет его с опубликованным `SHA256SUMS`, пробно запускает новый
бинарник, затем заменяет его и перезапускается. Без SSH.

> В Windows работающий `.exe` не может заменить сам себя, поэтому плашка версии
> лишь ведёт на страницу релиза. В Docker новый бинарник живёт в записываемом слое
> контейнера: он переживает `docker restart`, но пересоздание контейнера вернёт
> версию из образа — чтобы обновление закрепилось, скачайте новый образ.

### Поддерживаемые платформы

| Платформа | Архитектура | Статус |
|----------|--------------|---------|
| Linux    | amd64, arm64, armv7, armv6, armv5, 386, s390x | ✅ Поддерживается |
| Windows  | amd64, 386, arm64 | ✅ Поддерживается |
| macOS    | amd64, arm64 | 🚧 Экспериментально |

## Кластер из нескольких узлов

Одна панель может управлять остальными. Добавьте удалённый экземпляр 2S-UI на
странице **Узлы** (Nodes), указав его адрес и API-токен, и главная панель будет:

- **Следить за ним** — heartbeat раз в 5 секунд сообщает по каждому узлу: в сети,
  не в сети или ядро остановлено (панель доступна, но sing-box не работает).
- **Делить с ним пользователей** — клиенты главной панели, ссылающиеся на входящие
  узла, отправляются на этот узел и поддерживаются в синхронном состоянии, а трафик
  каждого узла сворачивается обратно в счётчики главной панели. Синхронизация
  ограничена группой `@cluster`, поэтому собственных локальных пользователей узла она
  не трогает.
- **Собирать его серверы в одну подписку** — одна ссылка подписки содержит и серверы
  главной панели, и серверы всех привязанных узлов.

Узел — это просто другой экземпляр 2S-UI, работающий через API v2 (заголовок `Token`):
никакого агента ставить не нужно, а единственная настройка на стороне узла — создать
этот API-токен в его собственной панели, так что уже существующие панели можно принять
как есть. Входящие, принятые с узла, становятся на главной панели репликами только для
чтения — редактируйте их на том узле, которому они принадлежат.

<details>
  <summary>Управление синхронизацией узлов через API</summary>

`POST <путь панели>apiv2/save` (по умолчанию путь панели — `/app/`, то есть
`/app/apiv2/save`) запускает немедленную рассылку по узлам, как в веб-интерфейсе,
только если в запросе передан `sync=true`; без него изменения клиентов и входящих
всё равно сойдутся благодаря ежечасной сверке.

</details>

## Домены и сертификаты

Всё, что касается TLS, живёт на вкладке **Домены и сертификаты** в настройках панели.
Панель и сервис подписок выбирают каждый свой домен, а пути к сертификатам следуют за
выбранным доменом — переносить пути к файлам вручную больше не нужно.

**🔐 Автоматические сертификаты (ACME / Let's Encrypt) — рекомендуется.** Введите
домен, укажите e-mail и нажмите «выпустить»: 2S-UI получит и будет автоматически
продлевать бесплатный сертификат Let's Encrypt, после чего панель будет доступна по
адресу `https://<your-domain>:2095/app`. Требуется, чтобы TCP-порт **80** был доступен
из интернета (HTTP-01 challenge). ACME работает только на Linux и скрыт в Windows.

<details>
  <summary>Как проходит выпуск и что учесть с портом 80 в Docker</summary>

Выпуск выполняется через **acme.sh** — при первом использовании панель сама установит
acme.sh (а также `socat`, необходимый для standalone-проверки) и включит автопродление
через собственную cron-задачу acme.sh, так что вам не нужно ничего настраивать в
планировщике.

Способ проверки по умолчанию — **auto**: standalone, если порт 80 свободен, иначе
задействуется уже работающий nginx, и при необходимости под `/etc/nginx/conf.d`
создаётся минимальный блок `server_name`. Можно явно выбрать **standalone** или
**nginx**, если хотите решать сами. При продлении сертификат перечитывается на лету —
перезапуск не нужен.

> Чтобы опубликовать порт 80 в Docker: раскомментируйте строку `80:80` в
> `docker-compose.yml` или добавьте `-p 80:80` в `docker run`. Сертификаты хранятся в
> `/root/cert/<домен>/` под именами `fullchain.pem` / `privkey.pem` и сохраняются при
> перезапусках (том в команде Docker выше как раз монтирует этот путь). Если домен/порт
> настроены неправильно, 2S-UI переключается на HTTP.

</details>

<details>
  <summary>Собственный сертификат</summary>

Сертификаты, которыми вы управляете сами — Cloudflare origin CA, корпоративный CA,
результат работы certbot — регистрируются на той же вкладке. 2S-UI проверяет, что файлы
читаются, что ключ соответствует сертификату и что сертификат действительно покрывает
домен; после этого домен становится доступен для выбора на вкладках «Интерфейс» и
«Подписка», как любой другой. Зарегистрированные сертификаты попадают в резервные копии
базы данных.

Чтобы выпустить сертификат самостоятельно через Certbot:

```bash
snap install core; snap refresh core
snap install --classic certbot
ln -s /snap/bin/certbot /usr/bin/certbot

certbot certonly --standalone --register-unsafely-without-email --non-interactive --agree-tos -d <Your Domain Name>
```

Затем зарегистрируйте полученные `fullchain.pem` / `privkey.pem` на вкладке
**Домены и сертификаты**.

</details>

<details>
  <summary>За обратным прокси</summary>

Включите **TLS терминируется обратным прокси**, и 2S-UI сам напишет vhost:
`/etc/nginx/conf.d/s-ui-proxy-<домен>.conf`, направленный на панель и с нужными
заголовками проксирования, проверит его через `nginx -t`, перезагрузит nginx и откатит
изменения, вернув собственное сообщение об ошибке nginx, если что-то не получится.
Сервер подписок может стоять за тем же прокси.

</details>

## Участие в разработке

См. [CONTRIBUTING.md](CONTRIBUTING.md) — там описаны настройка среды разработки,
соглашения по написанию кода, тестирование и процесс отправки pull request.

<details>
  <summary>Сборка и запуск из исходников</summary>

```shell
git clone https://github.com/shenaba/2s-ui
cd 2s-ui
./runSUI.sh
```

`build.sh` собирает фронтенд, копирует его в `web/html/` для `//go:embed` и собирает
бинарник с нужными build tags; `runSUI.sh` поверх этого сразу его запускает. Ручная
сборка требует тех же тегов — см. [CONTRIBUTING.md](CONTRIBUTING.md).

</details>

<details>
  <summary>Переменные окружения</summary>

| Переменная     |                      Тип                       | По умолчанию  |
| -------------- | :--------------------------------------------: | :------------ |
| SUI_LOG_LEVEL  | `"debug"` \| `"info"` \| `"warn"` \| `"error"` | `"info"`      |
| SUI_DEBUG      |                   `boolean`                    | `false`       |
| SUI_DB_FOLDER  |                    `string`                    | `"db"`        |
| SUI_BIN_FOLDER |                    `string`                    | `"bin"`       |

`SUI_BIN_FOLDER` читается только при миграции базы данных со старой схемы, где
sing-box запускался отдельным процессом; сейчас sing-box встроен в бинарник, и
каталога `bin/` во время работы не существует.

</details>

## Особая благодарность

- [@alireza0](https://github.com/alireza0)

## Звёзды со временем
[![Star History Chart](https://api.star-history.com/svg?repos=shenaba/2s-ui&type=Date)](https://star-history.com/#shenaba/2s-ui&Date)
