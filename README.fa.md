# <img src="frontend/public/assets/favicon.svg" width="44" height="44" align="texttop" alt=""> 2S-UI
[English](README.md) · [فارسی](README.fa.md) · [Tiếng Việt](README.vi.md) · [简体中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md) · [Русский](README.ru.md)

2S-UI یک پنل مدیریت متن‌باز برای [sing-box](https://github.com/SagerNet/sing-box) است و رابطی ساده و چندزبانه برای استقرار، پیکربندی و پایش انواع پروتکل‌های پراکسی و VPN در اختیار شما می‌گذارد — از یک VPS تنها تا استقرار چندنودی.

2S-UI به‌عنوان fork پروژه s-ui آغاز شد: کل frontend از نو نوشته شده و امکانات زیادی برای بهتر شدن تجربه کار با پنل به آن افزوده شده است.

![](https://img.shields.io/github/v/release/shenaba/2s-ui.svg)
[![Docker Pulls](https://img.shields.io/docker/pulls/shenaba/2s-ui.svg)](https://hub.docker.com/r/shenaba/2s-ui)
[![Go Report Card](https://goreportcard.com/badge/github.com/shenaba/2s-ui)](https://goreportcard.com/report/github.com/shenaba/2s-ui)
[![Downloads](https://img.shields.io/github/downloads/shenaba/2s-ui/total.svg)](https://github.com/shenaba/2s-ui/releases)
[![License](https://img.shields.io/badge/license-GPL%20V3-blue.svg?longCache=true)](https://www.gnu.org/licenses/gpl-3.0.en.html)

> **سلب مسئولیت:** این پروژه تنها برای یادگیری و تبادل نظر شخصی است؛ لطفاً از آن برای مقاصد غیرقانونی استفاده نکنید و آن را در محیط تولیدی (production) به کار نگیرید.

## امکانات

- **چندپروتکلی** — VLESS، VMess، Trojan، Shadowsocks، Hysteria2، TUIC، AnyTLS و
  بیشتر، هم در ورودی و هم در خروجی، به‌همراه endpointهای WireGuard، WARP و
  Tailscale ([فهرست کامل](#protocols)).
- **TLS در یک جا** — Reality، اثرانگشت‌های uTLS، XTLS، و گواهی‌هایی که یک‌بار ثبت
  می‌شوند و سپس برای هر inbound انتخاب می‌گردند.
- **قواعد مسیریابی** — تطبیق بر اساس دامنه، IP، پورت، پروتکل، پروسه، کاربر و
  rule-set، با ترکیب and/or؛ DNS هم فهرست قواعد مخصوص خودش را دارد.
- **مدیریت کلاینت‌ها** — سهمیه ترافیک، تاریخ انقضا، محدودیت IP، وضعیت آنلاین
  لحظه‌ای، و لینک اشتراک‌گذاری، QR Code و اشتراک تنها با یک کلیک.
- **آمار ترافیک** — به تفکیک inbound، کلاینت و outbound، با امکان بازنشانی.
- **اشتراک‌ها** — قالب‌های `link`، `json` و `clash`، همراه با گزارش مصرف و انقضا به
  اپلیکیشن کلاینت، و ادغام لینک‌های خارجی.
- **کلاستر چندنودی** — پایش وضعیت نودها، اشتراک کاربران میان آن‌ها و ادغام
  سرورهایشان در یک اشتراک واحد (در ادامه ببینید).
- **HTTPS خودکار** — صدور و تمدید گواهی Let's Encrypt، به‌همراه ساخت خودکار
  ریورس‌پراکسی nginx (در ادامه ببینید).
- **به‌روزرسانی با یک کلیک** — ارتقا در همان محل از داخل پنل، با بررسی checksum.
- **رابط بازنویسی‌شده** — frontend از صفر، کامپوننت‌های دست‌ساز، تم تیره و روشن، و
  شش زبان (از جمله RTL).

<details id="protocols">
  <summary>پروتکل‌های پشتیبانی‌شده</summary>

- عمومی: Mixed, SOCKS, HTTP/HTTPS, Direct, Tun, Redirect, TProxy
- مبتنی بر V2Ray: VLESS, VMess, Trojan, Shadowsocks (به‌همراه `plugin` / `plugin_opts`)
- سایر پروتکل‌ها: ShadowTLS, Hysteria, Hysteria2, Naive¹, TUIC, AnyTLS
- فقط outbound: Tor, SSH, Selector, URLTest
- Endpointها: WireGuard، WARP، Tailscale — با تست تأخیر برای هر endpoint یا همه با هم
- از XTLS پشتیبانی می‌شود و در فرم outbound قابلیت Hysteria port hopping وجود دارد

<sup>1</sup> پروتکل Naive به زنجیره ابزار cronet نیاز دارد که همه‌جا ساخته نمی‌شود: نسخه‌های
رسمی Linux فقط روی amd64، arm64، armv7 و 386 آن را دارند. روی armv6، armv5 و s390x یک
outbound از نوع Naive اعلام می‌کند که باینری بدون آن ساخته شده است.

</details>

## زبان‌ها

انگلیسی · فارسی · ویتنامی · چینی (ساده‌شده) · چینی (سنتی) · روسی

## تصاویر

!["Main"](frontend/media/main.png)

[سایر تصاویر رابط کاربری](frontend/screenshots.md)

## مستندات API

[ویکی مستندات API](https://github.com/shenaba/2s-ui/wiki/API-Documentation)

## اطلاعات نصب پیش‌فرض

| | پیش‌فرض |
| --- | --- |
| پنل | پورت `2095`، مسیر `/app/` |
| اشتراک | پورت `2096`، مسیر `/sub/` |
| نام کاربری / رمز عبور | `admin` / `admin` |

## نصب

### Linux/macOS

```sh
bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
```

زبان از `$LANG` پیروی می‌کند، یا یکی از `en`، `fa`، `ru`، `vi`، `zhcn`،
`zhtw` را بدهید:

```sh
SUI_LANG=fa bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
```

### Alpine Linux

آلپاین نه `bash` دارد و نه `curl`؛ اول آن‌ها را نصب کنید تا اسکریپت، آلپاین را
تشخیص دهد و پنل را به‌صورت سرویس OpenRC راه‌اندازی کند:

```sh
apk add bash curl
bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
```

### Windows

1. آخرین نسخه ویندوز را از [GitHub Releases](https://github.com/shenaba/2s-ui/releases/latest) دانلود کنید
2. فایل ZIP را استخراج کنید
3. `install-windows.bat` را به‌عنوان Administrator اجرا کنید
4. مراحل جادوگر نصب (wizard) را دنبال کنید
5. به پنل از طریق http://localhost:2095/app دسترسی پیدا کنید

### Docker

```shell
mkdir 2s-ui && cd 2s-ui
wget -q https://raw.githubusercontent.com/shenaba/2s-ui/main/docker-compose.yml
docker compose up -d
```

<details>
  <summary>بدون compose، یا ساخت image اختصاصی خودتان</summary>

اگر هنوز Docker نصب نیست:

```shell
curl -fsSL https://get.docker.com | sh
```

استفاده مستقیم از `docker run`:

```shell
mkdir 2s-ui && cd 2s-ui
docker run -itd \
    -p 2095:2095 -p 2096:2096 -p 443:443 \
    -v $PWD/db/:/app/db/ \
    -v $PWD/cert/:/root/cert/ \
    --name s-ui --restart=unless-stopped \
    ghcr.io/shenaba/2s-ui:latest
```

ساخت image اختصاصی خودتان:

```shell
git clone https://github.com/shenaba/2s-ui
docker build -t 2s-ui .
```

</details>

<details>
  <summary>نصب نسخه مشخص، نصب دستی، حذف</summary>

**نصب یک نسخه مشخص.** نسخه را به انتهای دستور نصب اضافه کنید، برای مثال `v1.5.5`:

```sh
VERSION=v1.5.5 && bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/$VERSION/install.sh) $VERSION
```

**نصب دستی — Linux/macOS**

1. آخرین نسخه 2S-UI متناسب با سیستم‌عامل/معماری خود را از GitHub دریافت کنید: [https://github.com/shenaba/2s-ui/releases/latest](https://github.com/shenaba/2s-ui/releases/latest)
2. **اختیاری** آخرین نسخه `s-ui.sh` را دریافت کنید [https://raw.githubusercontent.com/shenaba/2s-ui/main/s-ui.sh](https://raw.githubusercontent.com/shenaba/2s-ui/main/s-ui.sh)
3. **اختیاری** `s-ui.sh` را در `/usr/bin/s-ui` کپی کنید و `chmod +x /usr/bin/s-ui` را اجرا کنید.
4. فایل tar.gz مربوط به s-ui را در دایرکتوری دلخواه استخراج کنید و به دایرکتوری‌ای که فایل tar.gz را در آن استخراج کرده‌اید بروید.
5. فایل‌های *.service را در /etc/systemd/system/ کپی کنید و `systemctl daemon-reload` را اجرا کنید.
6. اجرای خودکار را فعال کرده و سرویس 2S-UI را با `systemctl enable s-ui --now` راه‌اندازی کنید
7. سرویس sing-box را با `systemctl enable sing-box --now` راه‌اندازی کنید

**نصب دستی — Windows**

1. آخرین نسخه ویندوز را از GitHub دریافت کنید: [https://github.com/shenaba/2s-ui/releases/latest](https://github.com/shenaba/2s-ui/releases/latest)
2. بسته مناسب ویندوز را دانلود کنید (برای مثال `s-ui-windows-amd64.zip`)
3. فایل ZIP را در دایرکتوری دلخواه استخراج کنید
4. `install-windows.bat` را به‌عنوان Administrator اجرا کنید
5. مراحل جادوگر نصب (wizard) را دنبال کنید
6. به پنل از طریق http://localhost:2095/app دسترسی پیدا کنید

**حذف**

```sh
sudo -i

systemctl disable s-ui  --now

rm -f /etc/systemd/system/sing-box.service
systemctl daemon-reload

rm -fr /usr/local/s-ui
rm /usr/bin/s-ui
```

</details>

### ارتقا

نسخه‌های جدید روی نشان نسخه در نوار کناری اعلام می‌شود — این بررسی در مرورگر
انجام می‌شود، بنابراین خودِ سرور پنل نیازی به دسترسی به GitHub ندارد. روی Linux (چه با
systemd و چه Docker) با یک کلیک ارتقا در همان محل انجام می‌شود: پنل نسخه جدید را دانلود
می‌کند، با `SHA256SUMS` منتشرشده تطبیق می‌دهد، باینری جدید را یک بار آزمایشی اجرا می‌کند،
سپس آن را جایگزین کرده و راه‌اندازی مجدد می‌شود. بدون SSH.

> در Windows یک `.exe` در حال اجرا نمی‌تواند خودش را جایگزین کند، بنابراین نشان نسخه
> فقط به صفحه انتشار لینک می‌دهد. در Docker باینری جدید در لایه قابل‌نوشتن کانتینر
> قرار می‌گیرد: با `docker restart` باقی می‌ماند، اما ساخت دوباره کانتینر به نسخه ایمیج
> برمی‌گردد — برای ماندگاری، ایمیج جدید را pull کنید.

### پلتفرم‌های پشتیبانی‌شده

| پلتفرم | معماری | وضعیت |
|----------|--------------|---------|
| Linux    | amd64, arm64, armv7, armv6, armv5, 386, s390x | ✅ پشتیبانی‌شده |
| Windows  | amd64, 386, arm64 | ✅ پشتیبانی‌شده |
| macOS    | amd64, arm64 | 🚧 آزمایشی |

## کلاستر چندنودی

یک پنل می‌تواند بقیه را مدیریت کند. در صفحه **نودها** (Nodes) یک نمونه راه‌دور 2S-UI را با
نشانی و یک توکن API اضافه کنید تا پنل اصلی (master):

- **آن را پایش کند** — یک heartbeat هر ۵ ثانیه وضعیت هر نود را آنلاین، آفلاین یا
  متوقف‌بودن هسته گزارش می‌کند (پنل در دسترس است اما sing-box اجرا نمی‌شود).
- **کاربران را با آن به اشتراک بگذارد** — کلاینت‌هایی که روی master به inboundهای آن
  نود ارجاع دارند به همان نود ارسال و همگام نگه داشته می‌شوند و ترافیک هر نود در
  شمارنده‌های master جمع می‌شود. همگام‌سازی محدود به گروه `@cluster` است، بنابراین
  کاربران محلی خودِ نود هرگز دست‌کاری نمی‌شوند.
- **سرورهای آن را در یک اشتراک واحد بیاورد** — یک لینک اشتراک، هم سرورهای master و هم
  سرورهای همه نودهای متصل را با هم دارد.

هر نود صرفاً یک نمونه دیگر از 2S-UI است که از طریق API نسخه ۲ (هدر `Token`) ارتباط
می‌گیرد: هیچ agent ای برای نصب لازم نیست و تنها کاری که در سمت نود باید انجام شود ساختن
همان توکن API در پنل خودِ آن است، بنابراین پنل‌های موجود را می‌توان همان‌طور که هستند تحویل
گرفت. inboundهایی که از یک نود تحویل گرفته می‌شوند روی master فقط‌خواندنی هستند — آن‌ها را
روی نود صاحبشان ویرایش کنید.

<details>
  <summary>هدایت همگام‌سازی نودها از طریق API</summary>

`POST <مسیر پنل>apiv2/save` (مسیر پنل به‌طور پیش‌فرض `/app/` است، یعنی
`/app/apiv2/save`) تنها زمانی ارسال فوری به نودها را — همان کاری که رابط وب می‌کند —
راه می‌اندازد که درخواست حاوی `sync=true` باشد؛ بدون آن، تغییرات کلاینت‌ها و inboundها
همچنان از طریق تطبیق ساعتی به هم می‌رسند.

</details>

## دامنه‌ها و گواهی‌ها

هر چیزی که به TLS مربوط است در زبانه **دامنه‌ها و گواهی‌ها** در تنظیمات پنل قرار دارد.
پنل و سرویس اشتراک هر کدام دامنه خودشان را انتخاب می‌کنند و مسیر گواهی‌ها به‌دنبال دامنه
انتخاب‌شده تعیین می‌شود — دیگر لازم نیست مسیر فایل‌ها را دستی جابه‌جا کنید.

**🔐 گواهی‌های خودکار (ACME / Let's Encrypt) — توصیه‌شده.** دامنه را وارد کنید، ایمیل را
بیفزایید و دکمه صدور را بزنید: 2S-UI یک گواهی رایگان Let's Encrypt می‌گیرد و به‌صورت
خودکار تمدید می‌کند، و پس از آن پنل از طریق `https://<your-domain>:2095/app` در دسترس
خواهد بود. نیازمند دسترس‌پذیری پورت TCP **۸۰** از اینترنت است (چالش HTTP-01). ACME فقط
روی Linux کار می‌کند و در Windows پنهان است.

<details>
  <summary>صدور گواهی چگونه انجام می‌شود، و نکته پورت ۸۰ در Docker</summary>

صدور گواهی از طریق **acme.sh** انجام می‌شود — پنل در نخستین استفاده، خودش acme.sh را نصب
می‌کند (همراه با `socat` که برای اعتبارسنجی standalone لازم است) و تمدید خودکار را با
cron job خودِ acme.sh فعال می‌کند، بنابراین لازم نیست شما هیچ زمان‌بندی‌ای تنظیم کنید.

روش اعتبارسنجی به‌صورت پیش‌فرض **auto** است — اگر پورت ۸۰ آزاد باشد از standalone
استفاده می‌کند، در غیر این صورت از nginx در حال اجرا کمک می‌گیرد و در صورت نیاز یک بلوک
`server_name` حداقلی زیر `/etc/nginx/conf.d` می‌سازد. اگر ترجیح می‌دهید خودتان تصمیم
بگیرید، می‌توانید صراحتاً **standalone** یا **nginx** را انتخاب کنید. هنگام تمدید، گواهی
به‌صورت داغ بارگذاری می‌شود و نیازی به راه‌اندازی مجدد نیست.

> برای انتشار پورت ۸۰ در Docker: در روش docker compose خط `80:80` را در
> `docker-compose.yml` از حالت کامنت خارج کنید، یا در روش docker run گزینه `-p 80:80` را
> اضافه کنید. گواهی‌ها در `/root/cert/<دامنه>/` با نام‌های `fullchain.pem` / `privkey.pem`
> ذخیره می‌شوند و پس از راه‌اندازی مجدد باقی می‌مانند (والیومِ دستور Docker بالا همین مسیر
> را بیرون نگاشت می‌کند). اگر دامنه/پورت نادرست پیکربندی شده باشد، 2S-UI به HTTP بازمی‌گردد.

</details>

<details>
  <summary>استفاده از گواهی شخصی</summary>

گواهی‌هایی که خودتان مدیریت می‌کنید — Cloudflare origin CA، یک CA سازمانی، یا خروجی
certbot — در همان زبانه قابل ثبت هستند. 2S-UI بررسی می‌کند که فایل‌ها خوانده شوند، کلید با
گواهی هم‌خوانی داشته باشد و گواهی واقعاً آن دامنه را پوشش دهد؛ پس از آن این دامنه هم مثل
بقیه در زبانه‌های «رابط» و «اشتراک» قابل انتخاب می‌شود. گواهی‌های ثبت‌شده در پشتیبان‌گیری
پایگاه داده هم گنجانده می‌شوند.

اگر ترجیح می‌دهید گواهی را خودتان با Certbot صادر کنید:

```bash
snap install core; snap refresh core
snap install --classic certbot
ln -s /snap/bin/certbot /usr/bin/certbot

certbot certonly --standalone --register-unsafely-without-email --non-interactive --agree-tos -d <Your Domain Name>
```

سپس `fullchain.pem` / `privkey.pem` حاصل را در زبانه **دامنه‌ها و گواهی‌ها** ثبت کنید.

</details>

<details>
  <summary>پشت ریورس‌پراکسی</summary>

کلید **TLS توسط ریورس‌پراکسی خاتمه می‌یابد** را روشن کنید تا 2S-UI خودش vhost را بنویسد:
`/etc/nginx/conf.d/s-ui-proxy-<دامنه>.conf`، که به پنل اشاره می‌کند و هدرهای لازم را
می‌فرستد، با `nginx -t` بررسی و سپس reload می‌شود، و اگر هر مرحله شکست بخورد همه‌چیز به
حالت قبل برمی‌گردد و خطای خودِ nginx بازگردانده می‌شود. سرویس اشتراک هم می‌تواند پشت همان
پراکسی قرار بگیرد.

</details>

## مشارکت

برای راه‌اندازی محیط توسعه، قراردادهای کدنویسی، تست و فرایند pull request به
[CONTRIBUTING.md](CONTRIBUTING.md) مراجعه کنید.

<details>
  <summary>ساخت و اجرا از روی سورس</summary>

```shell
git clone https://github.com/shenaba/2s-ui
cd 2s-ui
./runSUI.sh
```

`build.sh` فرانت‌اند را می‌سازد، آن را برای `//go:embed` در `web/html/` کپی می‌کند و
باینری را با build tagهای لازم می‌سازد؛ `runSUI.sh` هم همان را اجرا می‌کند. ساخت دستی به
همان tagها نیاز دارد — به [CONTRIBUTING.md](CONTRIBUTING.md) مراجعه کنید.

</details>

<details>
  <summary>متغیرهای محیطی</summary>

| متغیر          |                      نوع                       | پیش‌فرض        |
| -------------- | :--------------------------------------------: | :------------ |
| SUI_LOG_LEVEL  | `"debug"` \| `"info"` \| `"warn"` \| `"error"` | `"info"`      |
| SUI_DEBUG      |                   `boolean`                    | `false`       |
| SUI_DB_FOLDER  |                    `string`                    | `"db"`        |
| SUI_BIN_FOLDER |                    `string`                    | `"bin"`       |

`SUI_BIN_FOLDER` تنها هنگام مهاجرت پایگاه داده از ساختار قدیمی (که sing-box را به‌صورت
پروسه جدا اجرا می‌کرد) خوانده می‌شود؛ اکنون sing-box درون خود باینری تعبیه شده و در زمان
اجرا پوشه‌ای به نام `bin/` وجود ندارد.

</details>

## تشکر ویژه

- [@alireza0](https://github.com/alireza0)

## ستاره‌دهندگان در طول زمان
[![Star History Chart](https://api.star-history.com/svg?repos=shenaba/2s-ui&type=Date)](https://star-history.com/#shenaba/2s-ui&Date)
