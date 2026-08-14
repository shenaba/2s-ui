# <img src="frontend/public/assets/favicon.svg" width="44" height="44" align="texttop" alt=""> 2S-UI
[English](README.md) · [فارسی](README.fa.md) · [Tiếng Việt](README.vi.md) · [简体中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md) · [Русский](README.ru.md)

2S-UI là bảng điều khiển web mã nguồn mở dành cho [sing-box](https://github.com/SagerNet/sing-box), hướng tới việc triển khai và vận hành dịch vụ proxy tự lưu trữ. Cấu hình giao thức, luật định tuyến, người dùng và subscription, thống kê lưu lượng đều nằm trong cùng một giao diện — sáu ngôn ngữ và hai chế độ sáng/tối; chạy được trên một máy chủ đơn lẻ và cũng ghép được thành cụm.

2S-UI khởi đầu là một bản fork của [s-ui](https://github.com/alireza0/s-ui): toàn bộ frontend được viết lại, và trên nền đó là cụm đa node, cấp phát cùng gia hạn chứng chỉ ACME tự động, tự cập nhật ngay trong bảng điều khiển, và cập nhật người dùng tại chỗ không làm rớt các kết nối đang mở.

![](https://img.shields.io/github/v/release/shenaba/2s-ui.svg)
[![Docker Pulls](https://img.shields.io/docker/pulls/shenaba/2s-ui.svg)](https://hub.docker.com/r/shenaba/2s-ui)
[![Go Report Card](https://goreportcard.com/badge/github.com/shenaba/2s-ui)](https://goreportcard.com/report/github.com/shenaba/2s-ui)
[![Downloads](https://img.shields.io/github/downloads/shenaba/2s-ui/total.svg)](https://github.com/shenaba/2s-ui/releases)
[![License](https://img.shields.io/badge/license-GPL%20V3-blue.svg?longCache=true)](https://www.gnu.org/licenses/gpl-3.0.en.html)

> **Tuyên bố miễn trừ trách nhiệm:** Dự án này chỉ dành cho mục đích học tập và trao đổi cá nhân, vui lòng không sử dụng cho các mục đích bất hợp pháp, vui lòng không sử dụng trong môi trường production

**Nếu bạn thấy dự án này hữu ích, bạn có thể cho một**:star2:

## Tính năng

- **Đa giao thức** — VLESS, VMess, Trojan, Shadowsocks, Hysteria2, TUIC, AnyTLS và
  nhiều hơn nữa, cả inbound lẫn outbound, cùng các endpoint WireGuard/WARP/Tailscale
  ([danh sách đầy đủ](#protocols))
- **TLS tập trung một chỗ** — Reality, vân tay uTLS, XTLS; chứng chỉ đăng ký một
  lần rồi chọn cho từng inbound
- **Luật định tuyến dựng ngay trong giao diện** — khớp theo tên miền, IP, cổng,
  giao thức, tiến trình, người dùng hoặc rule-set, kết hợp bằng and/or. DNS có bộ
  luật riêng của nó.
- **Một client trên nhiều inbound cùng lúc**, mỗi client có hạn mức lưu lượng và
  ngày hết hạn riêng; vượt một trong hai là tự động bị tắt
- **Tự động hóa hạn mức** — đồng hồ có thể bắt đầu từ lần dùng đầu tiên và tự reset
  mỗi N ngày, tự đưa client đã cạn hạn mức trở lại
- **Giới hạn số IP mỗi client** — quy định một client được dùng bao nhiêu IP nguồn
  cùng lúc; phần dư bị ngắt và chặn trong chốc lát, không cần đến fail2ban
- **Cập nhật người dùng tại chỗ** — sửa client sẽ ghi lại bảng người dùng của
  inbound tại chỗ thay vì dựng lại listener, nên những người còn lại không rớt kết nối
- **Subscription** — các định dạng `link`, `json` và `clash`, trả lượng đã dùng và
  hạn dùng về ứng dụng client, gộp được cả liên kết bên ngoài
- **Cụm đa node** — giám sát các bảng điều khiển 2S-UI khác, dùng chung người dùng
  giữa chúng, gộp máy chủ của chúng vào cùng một subscription ([chi tiết](#cụm-đa-node))
- **HTTPS tự động** — cấp phát và gia hạn chứng chỉ Let's Encrypt, kèm reverse proxy
  nginx tự động ([chi tiết](#tên-miền-và-chứng-chỉ))
- **Cập nhật một cú nhấp** — nâng cấp tại chỗ ngay trong bảng điều khiển, có xác
  thực checksum
- **Bảng điều khiển thời gian thực** — tài nguyên hệ thống, lưu lượng, tỷ trọng giao
  thức, thông lượng mạng, tình trạng node; mỗi thẻ đều bật/tắt được
- **Truy cập và ngôn ngữ** — nhiều quản trị viên bảng điều khiển,
  [API token](https://github.com/shenaba/2s-ui/wiki/API-Documentation) có hạn dùng,
  giao diện sáng/tối, sáu ngôn ngữ

<details id="protocols">
  <summary>Các giao thức được hỗ trợ</summary>

- Chung: Mixed, SOCKS, HTTP/HTTPS, Direct, Tun, Redirect, TProxy
- Dựa trên V2Ray: VLESS, VMess, Trojan, Shadowsocks (kèm `plugin` / `plugin_opts`)
- Các giao thức khác: ShadowTLS, Hysteria, Hysteria2, Naive¹, TUIC, AnyTLS
- Chỉ outbound: Tor, SSH, Selector, URLTest
- Endpoints: WireGuard, WARP, Tailscale — có kiểm tra độ trễ cho từng endpoint hoặc cho tất cả cùng lúc
- Hỗ trợ XTLS, và form outbound có Hysteria port hopping

<sup>1</sup> Naive cần bộ công cụ cronet, vốn không build được ở mọi nơi: các bản phát hành
Linux chính thức chỉ kèm nó trên amd64, arm64, armv7 và 386. Trên armv6, armv5 và s390x, một
outbound Naive sẽ báo rằng binary được build mà không có nó.

</details>

<details>
  <summary>Ngôn ngữ</summary>

Tiếng Anh · Tiếng Ba Tư · Tiếng Việt · Tiếng Trung (Giản thể) · Tiếng Trung (Phồn thể) · Tiếng Nga

</details>

<details>
  <summary>Ảnh chụp màn hình</summary>

!["Main"](frontend/media/main.png)

Các ảnh chụp màn hình giao diện khác: [frontend/screenshots.md](frontend/screenshots.md)

</details>

## Cài đặt

### Linux/macOS

```sh
bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
```

Sau đó mở `http://<máy-chủ-của-bạn>:2095/app/` và đăng nhập bằng `admin` / `admin`.

| | Mặc định |
| --- | --- |
| Bảng điều khiển | cổng `2095`, đường dẫn `/app/` |
| Subscription | cổng `2096`, đường dẫn `/sub/` |
| Người dùng / mật khẩu | `admin` / `admin` |

Hỗ trợ cả systemd lẫn OpenRC (Alpine); trình cài đặt sẽ tự chọn đúng loại. Nó dùng
cùng sáu ngôn ngữ với bảng điều khiển: `en`, `fa`, `ru`, `vi`, `zhcn`, `zhtw`, mặc
định theo `$LANG` của hệ thống, hoặc bạn có thể chọn một ngôn ngữ và menu `s-ui` sẽ
ghi nhớ:

```sh
SUI_LANG=vi bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
```

### Windows

1. Tải bản phát hành Windows mới nhất từ [GitHub Releases](https://github.com/shenaba/2s-ui/releases/latest)
2. Giải nén tệp ZIP
3. Chạy `install-windows.bat` với quyền Administrator
4. Làm theo trình hướng dẫn cài đặt
5. Truy cập bảng điều khiển tại http://localhost:2095/app

### Docker

```shell
mkdir 2s-ui && cd 2s-ui
wget -q https://raw.githubusercontent.com/shenaba/2s-ui/main/docker-compose.yml
docker compose up -d
```

<details>
  <summary>Không dùng compose, hoặc tự build image</summary>

Nếu chưa cài Docker:

```shell
curl -fsSL https://get.docker.com | sh
```

Dùng `docker run` trực tiếp:

```shell
mkdir 2s-ui && cd 2s-ui
docker run -itd \
    -p 2095:2095 -p 2096:2096 -p 443:443 \
    -v $PWD/db/:/app/db/ \
    -v $PWD/cert/:/root/cert/ \
    --name s-ui --restart=unless-stopped \
    ghcr.io/shenaba/2s-ui:latest
```

Tự build image:

```shell
git clone https://github.com/shenaba/2s-ui
docker build -t 2s-ui .
```

</details>

<details>
  <summary>Cài phiên bản cũ, cài thủ công, gỡ cài đặt</summary>

**Cài một phiên bản cũ.** Thêm số phiên bản vào cuối lệnh cài đặt, ví dụ `v1.5.5`:

```sh
VERSION=v1.5.5 && bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/$VERSION/install.sh) $VERSION
```

**Cài đặt thủ công — Linux/macOS**

1. Lấy phiên bản 2S-UI mới nhất phù hợp với hệ điều hành/kiến trúc của bạn từ GitHub: [https://github.com/shenaba/2s-ui/releases/latest](https://github.com/shenaba/2s-ui/releases/latest)
2. **TÙY CHỌN** Lấy phiên bản mới nhất của `s-ui.sh` [https://raw.githubusercontent.com/shenaba/2s-ui/main/s-ui.sh](https://raw.githubusercontent.com/shenaba/2s-ui/main/s-ui.sh)
3. **TÙY CHỌN** Sao chép `s-ui.sh` vào `/usr/bin/s-ui` và chạy `chmod +x /usr/bin/s-ui`.
4. Giải nén tệp s-ui tar.gz vào một thư mục tùy chọn và di chuyển đến thư mục nơi bạn đã giải nén tệp tar.gz.
5. Sao chép các tệp *.service vào /etc/systemd/system/ và chạy `systemctl daemon-reload`.
6. Bật tự động khởi động và khởi động dịch vụ 2S-UI bằng `systemctl enable s-ui --now`
7. Khởi động dịch vụ sing-box bằng `systemctl enable sing-box --now`

**Cài đặt thủ công — Windows**

1. Lấy phiên bản Windows mới nhất từ GitHub: [https://github.com/shenaba/2s-ui/releases/latest](https://github.com/shenaba/2s-ui/releases/latest)
2. Tải gói Windows phù hợp (ví dụ: `s-ui-windows-amd64.zip`)
3. Giải nén tệp ZIP vào một thư mục tùy chọn
4. Chạy `install-windows.bat` với quyền Administrator
5. Làm theo trình hướng dẫn cài đặt
6. Truy cập bảng điều khiển tại http://localhost:2095/app

**Gỡ cài đặt**

```sh
sudo -i

systemctl disable s-ui  --now

rm -f /etc/systemd/system/sing-box.service
systemctl daemon-reload

rm -fr /usr/local/s-ui
rm /usr/bin/s-ui
```

</details>

### Nâng cấp

Bản phát hành mới sẽ được báo lên nhãn phiên bản ở thanh bên — việc kiểm tra do
trình duyệt thực hiện, nên máy chủ đặt bảng điều khiển không cần truy cập được
GitHub. Trên Linux (systemd hoặc Docker), chỉ một cú nhấp là nâng cấp tại chỗ: bảng
điều khiển tải bản phát hành về, đối chiếu với `SHA256SUMS` đã công bố, chạy thử
binary mới rồi thay thế và khởi động lại. Không cần SSH.

> Trên Windows, một `.exe` đang chạy không thể tự thay thế chính nó, nên nhãn
> phiên bản chỉ dẫn tới trang release. Trong Docker, binary mới nằm ở lớp ghi được
> của container: nó sống sót qua `docker restart`, nhưng tạo lại container sẽ quay
> về phiên bản của image — hãy pull image mới để giữ lâu dài.

### Nền tảng được hỗ trợ

| Nền tảng | Kiến trúc | Trạng thái |
|----------|--------------|---------|
| Linux    | amd64, arm64, armv7, armv6, armv5, 386, s390x | ✅ Được hỗ trợ |
| Windows  | amd64, 386, arm64 | ✅ Được hỗ trợ |
| macOS    | amd64, arm64 | 🚧 Thử nghiệm |

## Cụm đa node

Một bảng điều khiển có thể quản lý những cái còn lại. Thêm một instance 2S-UI từ xa
ở trang **Node** (Nodes) cùng địa chỉ và một API token, rồi bảng điều khiển master sẽ:

- **Giám sát nó** — nhịp tim 5 giây báo mỗi node là trực tuyến, ngoại tuyến, hoặc
  core đã dừng (bảng điều khiển vẫn truy cập được nhưng sing-box không chạy).
- **Dùng chung người dùng với nó** — các client trên master có tham chiếu tới inbound
  của node sẽ được đẩy sang node đó và giữ đồng bộ, lưu lượng của từng node được gộp
  ngược về bộ đếm của master. Việc đồng bộ giới hạn trong nhóm `@cluster`, nên người
  dùng cục bộ của chính node không bao giờ bị đụng tới.
- **Gộp máy chủ của nó vào một subscription** — một liên kết subscription mang theo cả
  máy chủ của master lẫn máy chủ của mọi node đã gắn.

Một node chỉ là một instance 2S-UI khác giao tiếp qua API v2 (header `Token`): không có
agent nào phải cài, và việc duy nhất cần làm ở phía node là tạo API token đó trong chính
bảng điều khiển của nó, nên các bảng điều khiển sẵn có có thể được tiếp quản nguyên trạng.
Inbound tiếp quản từ một node trở thành bản sao chỉ đọc trên master — hãy sửa chúng trên
chính node sở hữu chúng.

<details>
  <summary>Điều khiển đồng bộ node qua API</summary>

`POST <đường dẫn bảng điều khiển>apiv2/save` (mặc định đường dẫn là `/app/`, tức
`/app/apiv2/save`) chỉ kích hoạt việc đẩy ngay sang các node như giao diện web khi
request có kèm `sync=true`; không có nó, thay đổi về client và inbound vẫn sẽ hội tụ
qua đợt đối soát mỗi giờ.

</details>

## Tên miền và chứng chỉ

Mọi thứ liên quan tới TLS nằm ở tab **Tên miền và chứng chỉ** trong Cài đặt bảng điều
khiển. Bảng điều khiển và dịch vụ subscription mỗi bên chọn tên miền riêng, còn đường
dẫn chứng chỉ đi theo tên miền bạn chọn — không phải chép tay đường dẫn tệp nữa.

**🔐 Chứng chỉ tự động (ACME / Let's Encrypt) — khuyến nghị.** Nhập tên miền, thêm
email và bấm cấp phát: 2S-UI sẽ lấy về và tự động gia hạn chứng chỉ Let's Encrypt miễn
phí, sau đó bảng điều khiển truy cập được tại `https://<your-domain>:2095/app`. Yêu cầu
cổng TCP **80** có thể truy cập từ internet (HTTP-01 challenge). ACME chỉ hỗ trợ Linux
và bị ẩn trên Windows.

<details>
  <summary>Quá trình cấp phát diễn ra thế nào, và lưu ý về cổng 80 với Docker</summary>

Việc cấp phát chạy qua **acme.sh** — ở lần dùng đầu tiên, bảng điều khiển sẽ tự cài
acme.sh cho bạn (cùng với `socat`, cần cho xác thực standalone) và bật gia hạn tự động
bằng chính cron job của acme.sh, nên bạn không phải tự lập lịch gì cả.

Phương thức xác thực mặc định là **auto** — dùng standalone khi cổng 80 còn trống, ngược
lại mượn nginx đang chạy và tự tạo một khối `server_name` tối thiểu dưới
`/etc/nginx/conf.d` nếu chưa có. Bạn có thể chỉ định rõ **standalone** hoặc **nginx** nếu
muốn tự quyết. Khi gia hạn, chứng chỉ được nạp nóng; không cần khởi động lại.

> Để publish cổng 80 với Docker: bỏ ghi chú dòng `80:80` trong `docker-compose.yml`,
> hoặc thêm `-p 80:80` vào `docker run`. Chứng chỉ được lưu trong `/root/cert/<tên-miền>/`
> với tên tệp `fullchain.pem` / `privkey.pem` và vẫn tồn tại sau khi khởi động lại (volume
> trong lệnh Docker ở trên chính là để map đường dẫn này ra ngoài). Nếu tên miền/cổng được
> cấu hình sai, 2S-UI sẽ chuyển về HTTP.

</details>

<details>
  <summary>Dùng chứng chỉ của riêng bạn</summary>

Chứng chỉ do bạn tự quản lý — Cloudflare origin CA, CA nội bộ, hay kết quả từ certbot —
có thể được đăng ký ngay trong tab đó. 2S-UI kiểm tra tệp có đọc được không, khóa có khớp
với chứng chỉ không, và chứng chỉ có thực sự bao phủ tên miền không; sau đó tên miền này
có thể chọn được ở tab Giao diện và Subscription như mọi tên miền khác. Chứng chỉ đã đăng
ký được đưa vào các bản sao lưu cơ sở dữ liệu.

Nếu muốn tự cấp phát bằng Certbot:

```bash
snap install core; snap refresh core
snap install --classic certbot
ln -s /snap/bin/certbot /usr/bin/certbot

certbot certonly --standalone --register-unsafely-without-email --non-interactive --agree-tos -d <Your Domain Name>
```

Sau đó đăng ký `fullchain.pem` / `privkey.pem` thu được vào tab **Tên miền và chứng chỉ**.

</details>

<details>
  <summary>Đứng sau reverse proxy</summary>

Bật **TLS được kết thúc bởi reverse proxy** và 2S-UI sẽ viết vhost giúp bạn:
`/etc/nginx/conf.d/s-ui-proxy-<tên-miền>.conf`, trỏ về bảng điều khiển kèm đúng các header
chuyển tiếp, kiểm tra bằng `nginx -t`, reload, và khôi phục lại trạng thái cũ kèm chính
thông báo lỗi của nginx nếu có bước nào thất bại. Máy chủ subscription có thể đứng sau
cùng một proxy.

</details>

## Đóng góp

Xem [CONTRIBUTING.md](CONTRIBUTING.md) để biết về thiết lập phát triển, quy ước viết
mã, kiểm thử và quy trình pull request.

<details>
  <summary>Build và chạy từ mã nguồn</summary>

```shell
git clone https://github.com/shenaba/2s-ui
cd 2s-ui
./runSUI.sh
```

`build.sh` build frontend, chép nó vào `web/html/` cho `//go:embed`, rồi build binary
với đúng bộ build tags; `runSUI.sh` chạy tiếp binary đó. Build thủ công cũng cần đúng
bộ tags ấy — xem [CONTRIBUTING.md](CONTRIBUTING.md).

</details>

<details>
  <summary>Biến môi trường</summary>

| Biến           |                      Kiểu                      | Mặc định      |
| -------------- | :--------------------------------------------: | :------------ |
| SUI_LOG_LEVEL  | `"debug"` \| `"info"` \| `"warn"` \| `"error"` | `"info"`      |
| SUI_DEBUG      |                   `boolean`                    | `false`       |
| SUI_DB_FOLDER  |                    `string`                    | `"db"`        |
| SUI_BIN_FOLDER |                    `string`                    | `"bin"`       |

`SUI_BIN_FOLDER` chỉ được đọc khi migrate cơ sở dữ liệu từ bố cục cũ chạy sing-box
như tiến trình con; sing-box nay đã được nhúng vào binary và không có thư mục `bin/`
lúc chạy.

</details>

## Lời cảm ơn đặc biệt

- [@alireza0](https://github.com/alireza0)

## Số lượng Stargazers theo thời gian
[![Star History Chart](https://api.star-history.com/svg?repos=shenaba/2s-ui&type=Date)](https://star-history.com/#shenaba/2s-ui&Date)
