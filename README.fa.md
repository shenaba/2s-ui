# 2S-UI

[English](README.md) · [فارسی](README.fa.md) · [Tiếng Việt](README.vi.md) · [简体中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md) · [Русский](README.ru.md)

**یک پنل وب sing-box با نگهداری فعال برای مدیریت پراکسی چندپروتکلی، ارائه اشتراک، پایش ترافیک و استقرار خودمیزبان.**

![](https://img.shields.io/github/v/release/shenaba/2s-ui.svg)
[![Docker Pulls](https://img.shields.io/docker/pulls/shenaba/2s-ui.svg)](https://hub.docker.com/r/shenaba/2s-ui)
[![Go Report Card](https://goreportcard.com/badge/github.com/shenaba/2s-ui)](https://goreportcard.com/report/github.com/shenaba/2s-ui)
[![Downloads](https://img.shields.io/github/downloads/shenaba/2s-ui/total.svg)](https://github.com/shenaba/2s-ui/releases)
[![License](https://img.shields.io/badge/license-GPL%20V3-blue.svg?longCache=true)](https://www.gnu.org/licenses/gpl-3.0.en.html)

> **سلب مسئولیت:** این پروژه تنها برای یادگیری و تبادل نظر شخصی است؛ لطفاً از آن برای مقاصد غیرقانونی استفاده نکنید و آن را در محیط تولیدی (production) به کار نگیرید.

**اگر فکر می‌کنید این پروژه برای شما مفید است، شاید بد نباشد یک ستاره بدهید**:star2:

**مایل به مشارکت هستید؟** برای راه‌اندازی محیط توسعه، قراردادهای کدنویسی، تست و فرایند pull request به [CONTRIBUTING.md](CONTRIBUTING.md) مراجعه کنید.

2S-UI بر پایه [alireza0/s-ui](https://github.com/alireza0/s-ui) ساخته شده و به‌عنوان یک fork ادامه‌دار نگهداری می‌شود. این پروژه مسیر اصلی پنل را حفظ می‌کند و در همان حال پشتیبانی sing-box، قابلیت‌های چندپروتکلی، اسکریپت‌های استقرار و رفع اشکالات مستمر را به‌روزرسانی می‌کند. با تشکر از نویسنده اصلی و مشارکت‌کنندگان.

## مرور سریع
| امکانات                                |      فعال؟          |
| -------------------------------------- | :----------------: |
| چندپروتکلی                             | :heavy_check_mark: |
| چندزبانه                               | :heavy_check_mark: |
| چند کلاینت/ورودی                       | :heavy_check_mark: |
| رابط پیشرفته مسیریابی ترافیک           | :heavy_check_mark: |
| وضعیت کلاینت و ترافیک و سیستم          | :heavy_check_mark: |
| لینک اشتراک (link/json/clash + info)   | :heavy_check_mark: |
| **HTTPS خودکار (ACME / Let's Encrypt)** ✨ | :heavy_check_mark: |
| تم تیره/روشن                           | :heavy_check_mark: |
| رابط API                               | :heavy_check_mark: |

## پلتفرم‌های پشتیبانی‌شده
| پلتفرم | معماری | وضعیت |
|----------|--------------|---------|
| Linux    | amd64, arm64, armv7, armv6, armv5, 386, s390x | ✅ پشتیبانی‌شده |
| Windows  | amd64, 386, arm64 | ✅ پشتیبانی‌شده |
| macOS    | amd64, arm64 | 🚧 آزمایشی |

## تصاویر

!["Main"](https://github.com/shenaba/2s-ui-frontend/raw/main/media/main.png)

[سایر تصاویر رابط کاربری](https://github.com/shenaba/2s-ui-frontend/blob/main/screenshots.md)

## مستندات API

[ویکی مستندات API](https://github.com/shenaba/2s-ui/wiki/API-Documentation)

## اطلاعات نصب پیش‌فرض
- پورت پنل: 2095
- مسیر پنل: /app/
- پورت اشتراک: 2096
- مسیر اشتراک: /sub/
- نام کاربری/رمز عبور: admin

## نصب و ارتقا به آخرین نسخه

### Linux/macOS
```sh
bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
```

### Windows
1. آخرین نسخه ویندوز را از [GitHub Releases](https://github.com/shenaba/2s-ui/releases/latest) دانلود کنید
2. فایل ZIP را استخراج کنید
3. `install-windows.bat` را به‌عنوان Administrator اجرا کنید
4. مراحل جادوگر نصب (wizard) را دنبال کنید

## نصب نسخه قدیمی

**گام ۱:** برای نصب نسخه قدیمی موردنظرتان، نسخه را به انتهای دستور نصب اضافه کنید. برای مثال نسخه `1.0.0`:

```sh
VERSION=1.0.0 && bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/$VERSION/install.sh) $VERSION
```

## نصب دستی

### Linux/macOS
1. آخرین نسخه 2S-UI متناسب با سیستم‌عامل/معماری خود را از GitHub دریافت کنید: [https://github.com/shenaba/2s-ui/releases/latest](https://github.com/shenaba/2s-ui/releases/latest)
2. **اختیاری** آخرین نسخه `s-ui.sh` را دریافت کنید [https://raw.githubusercontent.com/shenaba/2s-ui/main/s-ui.sh](https://raw.githubusercontent.com/shenaba/2s-ui/main/s-ui.sh)
3. **اختیاری** `s-ui.sh` را در `/usr/bin/s-ui` کپی کنید و `chmod +x /usr/bin/s-ui` را اجرا کنید.
4. فایل tar.gz مربوط به s-ui را در دایرکتوری دلخواه استخراج کنید و به دایرکتوری‌ای که فایل tar.gz را در آن استخراج کرده‌اید بروید.
5. فایل‌های *.service را در /etc/systemd/system/ کپی کنید و `systemctl daemon-reload` را اجرا کنید.
6. اجرای خودکار را فعال کرده و سرویس 2S-UI را با `systemctl enable s-ui --now` راه‌اندازی کنید
7. سرویس sing-box را با `systemctl enable sing-box --now` راه‌اندازی کنید

### Windows
1. آخرین نسخه ویندوز را از GitHub دریافت کنید: [https://github.com/shenaba/2s-ui/releases/latest](https://github.com/shenaba/2s-ui/releases/latest)
2. بسته مناسب ویندوز را دانلود کنید (برای مثال `s-ui-windows-amd64.zip`)
3. فایل ZIP را در دایرکتوری دلخواه استخراج کنید
4. `install-windows.bat` را به‌عنوان Administrator اجرا کنید
5. مراحل جادوگر نصب (wizard) را دنبال کنید
6. به پنل از طریق http://localhost:2095/app دسترسی پیدا کنید

## حذف 2S-UI

```sh
sudo -i

systemctl disable s-ui  --now

rm -f /etc/systemd/system/sing-box.service
systemctl daemon-reload

rm -fr /usr/local/s-ui
rm /usr/bin/s-ui
```

## نصب با استفاده از Docker

<details>
   <summary>برای جزئیات کلیک کنید</summary>

### نحوه استفاده

**گام ۱:** نصب Docker

```shell
curl -fsSL https://get.docker.com | sh
```

**گام ۲:** نصب 2S-UI

> روش Docker compose

```shell
mkdir 2s-ui && cd 2s-ui
wget -q https://raw.githubusercontent.com/shenaba/2s-ui/main/docker-compose.yml
docker compose up -d
```

> استفاده از docker

```shell
mkdir 2s-ui && cd 2s-ui
docker run -itd \
    -p 2095:2095 -p 2096:2096 -p 443:443 \
    -v $PWD/db/:/app/db/ \
    -v $PWD/cert/:/root/cert/ \
    --name s-ui --restart=unless-stopped \
    ghcr.io/shenaba/2s-ui:latest
```

> ساخت image اختصاصی خودتان

```shell
git clone https://github.com/shenaba/2s-ui
git submodule update --init --recursive
docker build -t 2s-ui .
```

</details>

## اجرای دستی ( مشارکت )

<details>
   <summary>برای جزئیات کلیک کنید</summary>

### ساخت و اجرای کل پروژه
```shell
./runSUI.sh
```

### کلون کردن مخزن
```shell
# clone repository
git clone https://github.com/shenaba/2s-ui
# clone submodules
git submodule update --init --recursive
```


### - Frontend

برای کد frontend به [2s-ui-frontend](https://github.com/shenaba/2s-ui-frontend) مراجعه کنید

### - Backend
> لطفاً ابتدا یک‌بار frontend را بسازید!

برای ساخت backend:
```shell
# remove old frontend compiled files
rm -fr web/html/*
# apply new frontend compiled files
cp -R frontend/dist/ web/html/
# build
go build -o sui main.go
```

برای اجرای backend (از پوشه ریشه مخزن):
```shell
./sui
```

</details>

## زبان‌ها

- انگلیسی
- فارسی
- ویتنامی
- چینی (ساده‌شده)
- چینی (سنتی)
- روسی

## امکانات

- پروتکل‌های پشتیبانی‌شده:
  - عمومی:  Mixed, SOCKS, HTTP, HTTPS, Direct, Redirect, TProxy
  - مبتنی بر V2Ray: VLESS, VMess, Trojan, Shadowsocks
  - سایر پروتکل‌ها: ShadowTLS, Hysteria, Hysteria2, Naive, TUIC
- پشتیبانی از پروتکل‌های XTLS
- رابطی پیشرفته برای مسیریابی ترافیک، شامل PROXY Protocol، پراکسی External و Transparent، گواهی SSL و پورت
- رابطی پیشرفته برای پیکربندی inbound و outbound
- سقف ترافیک و تاریخ انقضای کلاینت‌ها
- نمایش کلاینت‌های آنلاین، inboundها و outboundها همراه با آمار ترافیک، و پایش وضعیت سیستم
- سرویس اشتراک با قابلیت افزودن لینک‌ها و اشتراک‌های خارجی
- HTTPS برای دسترسی امن به پنل وب و سرویس اشتراک (دامنه شخصی + گواهی SSL)
- **گواهی‌های SSL خودکار** — کافی است یک دامنه وارد کنید و 2S-UI به‌صورت خودکار یک گواهی رایگان Let's Encrypt را برای شما صادر و تمدید می‌کند (بدون certbot، بدون cron job)
- تم تیره/روشن

## متغیرهای محیطی

<details>
  <summary>برای جزئیات کلیک کنید</summary>

### نحوه استفاده

| متغیر          |                      نوع                       | پیش‌فرض        |
| -------------- | :--------------------------------------------: | :------------ |
| SUI_LOG_LEVEL  | `"debug"` \| `"info"` \| `"warn"` \| `"error"` | `"info"`      |
| SUI_DEBUG      |                   `boolean`                    | `false`       |
| SUI_BIN_FOLDER |                    `string`                    | `"bin"`       |
| SUI_DB_FOLDER  |                    `string`                    | `"db"`        |
| SINGBOX_API    |                    `string`                    | -             |

</details>

## گواهی SSL

### 🔐 گواهی‌های خودکار (ACME / Let's Encrypt) — توصیه‌شده

کافی است یک دامنه را در **تنظیمات پنل** وارد کنید (حالت گواهی ← **ACME**) و
2S-UI به‌صورت خودکار یک گواهی رایگان Let's Encrypt را صادر و تمدید می‌کند — بدون certbot،
بدون cron job. پنل وب و سرویس اشتراک را می‌توان به‌صورت مستقل فعال کرد.
پس از انجام این کار، پنل از طریق `https://<your-domain>:2095/app` در دسترس خواهد بود.

> نیازمند دسترس‌پذیری پورت TCP **80** از اینترنت است (چالش HTTP-01). برای انتشار پورت ۸۰ در
> Docker: در روش docker compose خط `80:80` را در `docker-compose.yml` از حالت کامنت خارج کنید،
> یا در روش docker run گزینه `-p 80:80` را اضافه کنید. گواهی‌ها در پوشه `cert/` ذخیره می‌شوند و
> پس از راه‌اندازی مجدد باقی می‌مانند. اگر دامنه/پورت نادرست پیکربندی شده باشد، 2S-UI به HTTP بازمی‌گردد.

<details>
  <summary>ترجیح می‌دهید گواهی‌ها را خودتان مدیریت کنید؟ (Certbot)</summary>

### Certbot

```bash
snap install core; snap refresh core
snap install --classic certbot
ln -s /snap/bin/certbot /usr/bin/certbot

certbot certonly --standalone --register-unsafely-without-email --non-interactive --agree-tos -d <Your Domain Name>
```

</details>

## ستاره‌دهندگان در طول زمان
[![Stargazers over time](https://starchart.cc/shenaba/2s-ui.svg)](https://starchart.cc/shenaba/2s-ui)
