# 2S-UI

[English](README.md) · [فارسی](README.fa.md) · [Tiếng Việt](README.vi.md) · [简体中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md) · [Русский](README.ru.md)

**Активно поддерживаемая веб-панель sing-box для управления мультипротокольными прокси, доставки подписок, мониторинга трафика и самостоятельного развёртывания.**

![](https://img.shields.io/github/v/release/shenaba/2s-ui.svg)
[![Container image](https://img.shields.io/badge/container-ghcr.io%2Fshenaba%2F2s--ui-blue?logo=docker)](https://github.com/shenaba/2s-ui/actions/workflows/docker.yml)
[![Docker Pulls](https://img.shields.io/docker/pulls/shenaba/2s-ui.svg)](https://hub.docker.com/r/shenaba/2s-ui)
[![Go Report Card](https://goreportcard.com/badge/github.com/shenaba/2s-ui)](https://goreportcard.com/report/github.com/shenaba/2s-ui)
[![Downloads](https://img.shields.io/github/downloads/shenaba/2s-ui/total.svg)](https://github.com/shenaba/2s-ui/releases)
[![License](https://img.shields.io/badge/license-GPL%20V3-blue.svg?longCache=true)](https://www.gnu.org/licenses/gpl-3.0.en.html)

> **Отказ от ответственности:** Этот проект предназначен только для личного обучения и общения. Пожалуйста, не используйте его в незаконных целях и не используйте его в производственной среде.

**Если вы считаете, что этот проект вам полезен, можете поставить**:star2:

**Хотите внести свой вклад?** См. [CONTRIBUTING.md](CONTRIBUTING.md) — там описаны настройка среды разработки, соглашения по написанию кода, тестирование и процесс отправки pull request.

2S-UI основан на [alireza0/s-ui](https://github.com/alireza0/s-ui) и поддерживается как продолжающийся форк. Он сохраняет исходное направление панели, обновляя при этом поддержку sing-box, мультипротокольные возможности, скрипты развёртывания и постоянные исправления. Спасибо оригинальному автору и участникам.

## Краткий обзор
| Возможности                            |      Включено?     |
| -------------------------------------- | :----------------: |
| Мультипротокольность                   | :heavy_check_mark: |
| Многоязычность                         | :heavy_check_mark: |
| Несколько клиентов/входящих            | :heavy_check_mark: |
| Расширенный интерфейс маршрутизации трафика | :heavy_check_mark: |
| Статус клиентов, трафика и системы     | :heavy_check_mark: |
| Ссылка подписки (link/json/clash + информация)| :heavy_check_mark: |
| **Автоматический HTTPS (ACME / Let's Encrypt)** ✨ | :heavy_check_mark: |
| Тёмная/светлая тема                    | :heavy_check_mark: |
| API-интерфейс                          | :heavy_check_mark: |

## Поддерживаемые платформы
| Платформа | Архитектура | Статус |
|----------|--------------|---------|
| Linux    | amd64, arm64, armv7, armv6, armv5, 386, s390x | ✅ Поддерживается |
| Windows  | amd64, 386, arm64 | ✅ Поддерживается |
| macOS    | amd64, arm64 | 🚧 Экспериментально |

## Скриншоты

!["Main"](https://github.com/shenaba/2s-ui-frontend/raw/main/media/main.png)

[Другие скриншоты интерфейса](https://github.com/shenaba/2s-ui-frontend/blob/main/screenshots.md)

## Документация API

[Wiki с документацией API](https://github.com/shenaba/2s-ui/wiki/API-Documentation)

## Параметры установки по умолчанию
- Порт панели: 2095
- Путь панели: /app/
- Порт подписки: 2096
- Путь подписки: /sub/
- Пользователь/Пароль: admin

## Установка и обновление до последней версии

### Linux/macOS
```sh
bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
```

### Windows
1. Скачайте последний релиз для Windows с [GitHub Releases](https://github.com/shenaba/2s-ui/releases/latest)
2. Распакуйте ZIP-файл
3. Запустите `install-windows.bat` от имени администратора
4. Следуйте указаниям мастера установки

## Установка устаревшей версии

**Шаг 1:** Чтобы установить нужную устаревшую версию, добавьте версию в конец команды установки. Например, версия `1.0.0`:

```sh
VERSION=1.0.0 && bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/$VERSION/install.sh) $VERSION
```

## Ручная установка

### Linux/macOS
1. Получите последнюю версию 2S-UI для вашей ОС/архитектуры с GitHub: [https://github.com/shenaba/2s-ui/releases/latest](https://github.com/shenaba/2s-ui/releases/latest)
2. **НЕОБЯЗАТЕЛЬНО** Получите последнюю версию `s-ui.sh` [https://raw.githubusercontent.com/shenaba/2s-ui/main/s-ui.sh](https://raw.githubusercontent.com/shenaba/2s-ui/main/s-ui.sh)
3. **НЕОБЯЗАТЕЛЬНО** Скопируйте `s-ui.sh` в `/usr/bin/s-ui` и выполните `chmod +x /usr/bin/s-ui`.
4. Распакуйте файл s-ui tar.gz в выбранный каталог и перейдите в каталог, куда вы распаковали файл tar.gz.
5. Скопируйте файлы *.service в /etc/systemd/system/ и выполните `systemctl daemon-reload`.
6. Включите автозапуск и запустите службу 2S-UI с помощью `systemctl enable s-ui --now`
7. Запустите службу sing-box с помощью `systemctl enable sing-box --now`

### Windows
1. Получите последнюю версию для Windows с GitHub: [https://github.com/shenaba/2s-ui/releases/latest](https://github.com/shenaba/2s-ui/releases/latest)
2. Скачайте подходящий пакет для Windows (например, `s-ui-windows-amd64.zip`)
3. Распакуйте ZIP-файл в выбранный каталог
4. Запустите `install-windows.bat` от имени администратора
5. Следуйте указаниям мастера установки
6. Откройте панель по адресу http://localhost:2095/app

## Удаление 2S-UI

```sh
sudo -i

systemctl disable s-ui  --now

rm -f /etc/systemd/system/sing-box.service
systemctl daemon-reload

rm -fr /usr/local/s-ui
rm /usr/bin/s-ui
```

## Установка с помощью Docker

<details>
   <summary>Нажмите для подробностей</summary>

### Использование

**Шаг 1:** Установите Docker

```shell
curl -fsSL https://get.docker.com | sh
```

**Шаг 2:** Установите 2S-UI

> Способ через Docker Compose

```shell
mkdir 2s-ui && cd 2s-ui
wget -q https://raw.githubusercontent.com/shenaba/2s-ui/main/docker-compose.yml
docker compose up -d
```

> Использование docker

```shell
mkdir 2s-ui && cd 2s-ui
docker run -itd \
    -p 2095:2095 -p 2096:2096 -p 443:443 \
    -v $PWD/db/:/app/db/ \
    -v $PWD/cert/:/root/cert/ \
    --name s-ui --restart=unless-stopped \
    ghcr.io/shenaba/2s-ui:latest
```

> Сборка собственного образа

```shell
git clone https://github.com/shenaba/2s-ui
git submodule update --init --recursive
docker build -t 2s-ui .
```

</details>

## Ручной запуск ( для участников )

<details>
   <summary>Нажмите для подробностей</summary>

### Сборка и запуск всего проекта
```shell
./runSUI.sh
```

### Клонирование репозитория
```shell
# клонировать репозиторий
git clone https://github.com/shenaba/2s-ui
# клонировать подмодули
git submodule update --init --recursive
```


### - Фронтенд

Код фронтенда находится в [2s-ui-frontend](https://github.com/shenaba/2s-ui-frontend)

### - Бэкенд
> Пожалуйста, сначала один раз соберите фронтенд!

Чтобы собрать бэкенд:
```shell
# удалить старые скомпилированные файлы фронтенда
rm -fr web/html/*
# применить новые скомпилированные файлы фронтенда
cp -R frontend/dist/ web/html/
# сборка
go build -o sui main.go
```

Чтобы запустить бэкенд (из корневого каталога репозитория):
```shell
./sui
```

</details>

## Языки

- Английский
- Фарси
- Вьетнамский
- Китайский (упрощённый)
- Китайский (традиционный)
- Русский

## Возможности

- Поддерживаемые протоколы:
  - Общие:  Mixed, SOCKS, HTTP, HTTPS, Direct, Redirect, TProxy
  - На основе V2Ray: VLESS, VMess, Trojan, Shadowsocks
  - Другие протоколы: ShadowTLS, Hysteria, Hysteria2, Naive, TUIC
- Поддержка протоколов XTLS
- Расширенный интерфейс для маршрутизации трафика, включающий PROXY Protocol, внешний, прозрачный прокси, SSL-сертификат и порт
- Расширенный интерфейс для настройки входящих и исходящих соединений
- Лимит трафика клиентов и срок действия
- Отображение онлайн-клиентов, входящих и исходящих соединений со статистикой трафика, а также мониторинг состояния системы
- Сервис подписок с возможностью добавления внешних ссылок и подписок
- HTTPS для безопасного доступа к веб-панели и сервису подписок (собственный домен + SSL-сертификат)
- **Автоматические SSL-сертификаты** — просто введите домен, и 2S-UI выпустит и автоматически продлит для вас бесплатный сертификат Let's Encrypt (без certbot, без cron-задач)
- Тёмная/светлая тема

## Переменные окружения

<details>
  <summary>Нажмите для подробностей</summary>

### Использование

| Переменная     |                      Тип                       | По умолчанию  |
| -------------- | :--------------------------------------------: | :------------ |
| SUI_LOG_LEVEL  | `"debug"` \| `"info"` \| `"warn"` \| `"error"` | `"info"`      |
| SUI_DEBUG      |                   `boolean`                    | `false`       |
| SUI_BIN_FOLDER |                    `string`                    | `"bin"`       |
| SUI_DB_FOLDER  |                    `string`                    | `"db"`        |
| SINGBOX_API    |                    `string`                    | -             |

</details>

## SSL-сертификат

### 🔐 Автоматические сертификаты (ACME / Let's Encrypt) — Рекомендуется

Просто введите домен в **Настройках панели** (режим сертификата → **ACME**), и
2S-UI автоматически выпустит и продлит бесплатный сертификат Let's Encrypt — без certbot,
без cron-задач. Веб-панель и сервис подписок можно включать независимо друг от друга.
После завершения панель будет доступна по адресу `https://<your-domain>:2095/app`.

> Требуется, чтобы TCP-порт **80** был доступен из интернета (HTTP-01 challenge). Чтобы
> опубликовать порт 80 в Docker: раскомментируйте строку `80:80` в `docker-compose.yml`
> или добавьте `-p 80:80` в `docker run`. Сертификаты хранятся в каталоге `cert/` и сохраняются при
> перезапусках. Если домен/порт настроены неправильно, 2S-UI переключается на HTTP.

<details>
  <summary>Предпочитаете управлять сертификатами самостоятельно? (Certbot)</summary>

### Certbot

```bash
snap install core; snap refresh core
snap install --classic certbot
ln -s /snap/bin/certbot /usr/bin/certbot

certbot certonly --standalone --register-unsafely-without-email --non-interactive --agree-tos -d <Your Domain Name>
```

</details>

## Звёзды со временем
[![Stargazers over time](https://starchart.cc/shenaba/2s-ui.svg)](https://starchart.cc/shenaba/2s-ui)
