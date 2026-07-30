# 2S-UI
[English](README.md) · [فارسی](README.fa.md) · [Tiếng Việt](README.vi.md) · [简体中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md) · [Русский](README.ru.md)

**Một bảng điều khiển web sing-box được duy trì tích cực để quản lý proxy đa giao thức, phân phối subscription, giám sát lưu lượng và triển khai tự lưu trữ.**

![](https://img.shields.io/github/v/release/shenaba/2s-ui.svg)
[![Docker Pulls](https://img.shields.io/docker/pulls/shenaba/2s-ui.svg)](https://hub.docker.com/r/shenaba/2s-ui)
[![Go Report Card](https://goreportcard.com/badge/github.com/shenaba/2s-ui)](https://goreportcard.com/report/github.com/shenaba/2s-ui)
[![Downloads](https://img.shields.io/github/downloads/shenaba/2s-ui/total.svg)](https://github.com/shenaba/2s-ui/releases)
[![License](https://img.shields.io/badge/license-GPL%20V3-blue.svg?longCache=true)](https://www.gnu.org/licenses/gpl-3.0.en.html)

> **Tuyên bố miễn trừ trách nhiệm:** Dự án này chỉ dành cho mục đích học tập và trao đổi cá nhân, vui lòng không sử dụng cho các mục đích bất hợp pháp, vui lòng không sử dụng trong môi trường production

**Nếu bạn thấy dự án này hữu ích, bạn có thể cho một**:star2:

**Muốn đóng góp?** Xem [CONTRIBUTING.md](CONTRIBUTING.md) để biết về thiết lập phát triển, quy ước viết mã, kiểm thử và quy trình pull request.

2S-UI dựa trên [alireza0/s-ui](https://github.com/alireza0/s-ui) và được duy trì như một bản fork tiếp nối. Nó giữ nguyên định hướng của bảng điều khiển gốc đồng thời cập nhật hỗ trợ sing-box, khả năng đa giao thức, các script triển khai và các bản sửa lỗi liên tục. Cảm ơn tác giả gốc và những người đóng góp.

## Tổng quan nhanh
| Tính năng                              |       Bật?         |
| -------------------------------------- | :----------------: |
| Đa giao thức                           | :heavy_check_mark: |
| Đa ngôn ngữ                            | :heavy_check_mark: |
| Đa client/inbound                      | :heavy_check_mark: |
| Giao diện định tuyến lưu lượng nâng cao | :heavy_check_mark: |
| Trạng thái client, lưu lượng và hệ thống | :heavy_check_mark: |
| Liên kết subscription (link/json/clash + info)| :heavy_check_mark: |
| **Cụm đa node (dùng chung người dùng giữa các máy chủ)** ✨ | :heavy_check_mark: |
| **HTTPS tự động (ACME / Let's Encrypt)** ✨ | :heavy_check_mark: |
| **Tự động tạo reverse proxy nginx** ✨   | :heavy_check_mark: |
| **Tự cập nhật ngay trong bảng điều khiển** ✨ | :heavy_check_mark: |
| Giao diện sáng/tối                      | :heavy_check_mark: |
| Giao diện API                          | :heavy_check_mark: |

## Nền tảng được hỗ trợ
| Nền tảng | Kiến trúc | Trạng thái |
|----------|--------------|---------|
| Linux    | amd64, arm64, armv7, armv6, armv5, 386, s390x | ✅ Được hỗ trợ |
| Windows  | amd64, 386, arm64 | ✅ Được hỗ trợ |
| macOS    | amd64, arm64 | 🚧 Thử nghiệm |

## Ảnh chụp màn hình

!["Main"](frontend/media/main.png)

[Các ảnh chụp màn hình giao diện khác](frontend/screenshots.md)

## Tài liệu API

[Wiki Tài liệu API](https://github.com/shenaba/2s-ui/wiki/API-Documentation)

## Thông tin cài đặt mặc định
- Cổng bảng điều khiển: 2095
- Đường dẫn bảng điều khiển: /app/
- Cổng subscription: 2096
- Đường dẫn subscription: /sub/
- Người dùng/Mật khẩu: admin

## Cài đặt và nâng cấp lên phiên bản mới nhất

### Ngay trong bảng điều khiển (chỉ để nâng cấp)

Mỗi lần tải trang, **trình duyệt** kiểm tra bản phát hành mới trên GitHub và báo lên
nhãn phiên bản ở thanh bên — bước này chạy ở phía client, nên máy chủ đặt bảng điều
khiển không cần truy cập được GitHub thì thông báo vẫn hiện. Việc cài đặt thì ở phía
máy chủ: trên Linux (máy chủ thường cần systemd, hoặc Docker) chỉ một cú nhấp là bảng
điều khiển tải bản phát hành về, đối chiếu với `SHA256SUMS` đã công bố, chạy thử binary
mới rồi thay thế tại chỗ và khởi động lại. Không cần SSH, không cần script cài đặt.

> Trên Windows, một `.exe` đang chạy không thể tự thay thế chính nó, nên nhãn
> phiên bản chỉ dẫn tới trang release. Trong Docker, binary mới nằm ở lớp ghi được
> của container: nó sống sót qua `docker restart`, nhưng tạo lại container sẽ quay
> về phiên bản của image — hãy pull image mới để giữ lâu dài.

### Linux/macOS
```sh
bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
```

### Windows
1. Tải bản phát hành Windows mới nhất từ [GitHub Releases](https://github.com/shenaba/2s-ui/releases/latest)
2. Giải nén tệp ZIP
3. Chạy `install-windows.bat` với quyền Administrator
4. Làm theo trình hướng dẫn cài đặt

## Cài đặt phiên bản cũ

**Bước 1:** Để cài đặt phiên bản cũ mà bạn mong muốn, thêm số phiên bản vào cuối lệnh cài đặt. Ví dụ, phiên bản `v1.5.5`:

```sh
VERSION=v1.5.5 && bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/$VERSION/install.sh) $VERSION
```

## Cài đặt thủ công

### Linux/macOS
1. Lấy phiên bản 2S-UI mới nhất phù hợp với hệ điều hành/kiến trúc của bạn từ GitHub: [https://github.com/shenaba/2s-ui/releases/latest](https://github.com/shenaba/2s-ui/releases/latest)
2. **TÙY CHỌN** Lấy phiên bản mới nhất của `s-ui.sh` [https://raw.githubusercontent.com/shenaba/2s-ui/main/s-ui.sh](https://raw.githubusercontent.com/shenaba/2s-ui/main/s-ui.sh)
3. **TÙY CHỌN** Sao chép `s-ui.sh` vào `/usr/bin/s-ui` và chạy `chmod +x /usr/bin/s-ui`.
4. Giải nén tệp s-ui tar.gz vào một thư mục tùy chọn và di chuyển đến thư mục nơi bạn đã giải nén tệp tar.gz.
5. Sao chép các tệp *.service vào /etc/systemd/system/ và chạy `systemctl daemon-reload`.
6. Bật tự động khởi động và khởi động dịch vụ 2S-UI bằng `systemctl enable s-ui --now`
7. Khởi động dịch vụ sing-box bằng `systemctl enable sing-box --now`

### Windows
1. Lấy phiên bản Windows mới nhất từ GitHub: [https://github.com/shenaba/2s-ui/releases/latest](https://github.com/shenaba/2s-ui/releases/latest)
2. Tải gói Windows phù hợp (ví dụ: `s-ui-windows-amd64.zip`)
3. Giải nén tệp ZIP vào một thư mục tùy chọn
4. Chạy `install-windows.bat` với quyền Administrator
5. Làm theo trình hướng dẫn cài đặt
6. Truy cập bảng điều khiển tại http://localhost:2095/app

## Gỡ cài đặt 2S-UI

```sh
sudo -i

systemctl disable s-ui  --now

rm -f /etc/systemd/system/sing-box.service
systemctl daemon-reload

rm -fr /usr/local/s-ui
rm /usr/bin/s-ui
```

## Cài đặt bằng Docker

<details>
   <summary>Nhấn để xem chi tiết</summary>

### Cách sử dụng

**Bước 1:** Cài đặt Docker

```shell
curl -fsSL https://get.docker.com | sh
```

**Bước 2:** Cài đặt 2S-UI

> Phương pháp Docker compose

```shell
mkdir 2s-ui && cd 2s-ui
wget -q https://raw.githubusercontent.com/shenaba/2s-ui/main/docker-compose.yml
docker compose up -d
```

> Sử dụng docker

```shell
mkdir 2s-ui && cd 2s-ui
docker run -itd \
    -p 2095:2095 -p 2096:2096 -p 443:443 \
    -v $PWD/db/:/app/db/ \
    -v $PWD/cert/:/root/cert/ \
    --name s-ui --restart=unless-stopped \
    ghcr.io/shenaba/2s-ui:latest
```

> Tự xây dựng image của riêng bạn

```shell
git clone https://github.com/shenaba/2s-ui
docker build -t 2s-ui .
```

</details>

## Chạy thủ công ( đóng góp )

<details>
   <summary>Nhấn để xem chi tiết</summary>

### Xây dựng và chạy toàn bộ dự án
```shell
./runSUI.sh
```

### Sao chép repository
```shell
# sao chép repository
git clone https://github.com/shenaba/2s-ui
```


### - Frontend

Mã frontend nằm trong thư mục [`frontend/`](frontend) của repository này.

### - Backend
> Vui lòng xây dựng frontend một lần trước!

Để xây dựng backend:
```shell
# xóa các tệp frontend đã biên dịch cũ
rm -fr web/html/*
# áp dụng các tệp frontend đã biên dịch mới
cp -R frontend/dist/ web/html/
# xây dựng
go build -o sui main.go
```

Để chạy backend (từ thư mục gốc của repository):
```shell
./sui
```

</details>

## Ngôn ngữ

- Tiếng Anh
- Tiếng Ba Tư
- Tiếng Việt
- Tiếng Trung (Giản thể)
- Tiếng Trung (Phồn thể)
- Tiếng Nga

## Tính năng

- Các giao thức được hỗ trợ:
  - Chung: Mixed, SOCKS, HTTP/HTTPS, Direct, Tun, Redirect, TProxy
  - Dựa trên V2Ray: VLESS, VMess, Trojan, Shadowsocks (kèm `plugin` / `plugin_opts`)
  - Các giao thức khác: ShadowTLS, Hysteria, Hysteria2, Naive¹, TUIC, AnyTLS
  - Chỉ outbound: Tor, SSH, Selector, URLTest
  - Endpoints: WireGuard, WARP, Tailscale — có kiểm tra độ trễ cho từng endpoint hoặc cho tất cả cùng lúc

  <sup>1</sup> Naive cần bộ công cụ cronet, vốn không build được ở mọi nơi: các bản phát hành
  Linux chính thức chỉ kèm nó trên amd64, arm64, armv7 và 386. Trên armv6, armv5 và s390x, một
  outbound Naive sẽ báo rằng binary được build mà không có nó.

- Hỗ trợ các giao thức XTLS
- Giao diện nâng cao để định tuyến lưu lượng, tích hợp PROXY Protocol, External và Transparent Proxy, SSL Certificate và Port
- Giao diện nâng cao để cấu hình inbound và outbound
- Giới hạn lưu lượng và ngày hết hạn của client; bật hoặc tắt một client ngay từ danh sách
- **Cập nhật người dùng tại chỗ** — trên VLESS, VMess, Trojan, Shadowsocks, AnyTLS, Hysteria, Hysteria2 và TUIC, việc thêm, sửa hoặc xóa client sẽ cập nhật bảng người dùng của inbound tại chỗ thay vì dựng lại listener, nhờ đó những người dùng còn lại không bị rớt kết nối — điều này quan trọng nhất với các giao thức nền QUIC, nơi dựng lại listener sẽ ngắt toàn bộ phiên. Các loại inbound khác vẫn khởi động lại
- Hysteria port hopping trong form outbound
- Hiển thị client đang trực tuyến, inbound và outbound với thống kê lưu lượng, và giám sát trạng thái hệ thống
- Dịch vụ subscription với khả năng thêm liên kết và subscription bên ngoài
- **Cụm đa node** — giám sát các bảng điều khiển 2S-UI khác, dùng chung người dùng giữa chúng, và gộp máy chủ của chúng vào cùng một subscription (xem bên dưới)
- HTTPS để truy cập an toàn vào bảng điều khiển web và dịch vụ subscription (tên miền tự cung cấp + chứng chỉ SSL)
- **Chứng chỉ SSL tự động** — chỉ cần nhập tên miền và 2S-UI sẽ cấp phát và tự động gia hạn chứng chỉ Let's Encrypt miễn phí cho bạn (acme.sh được bảng điều khiển tự cài và gọi; bạn không phải lập lịch gì)
- **Reverse proxy nginx tự động** — 2S-UI tự viết và kiểm tra vhost khi bạn đặt bảng điều khiển sau một proxy
- **Tự cập nhật ngay trong bảng điều khiển** qua các bản phát hành GitHub đã xác thực checksum
- Giao diện sáng/tối

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

## Biến môi trường

<details>
  <summary>Nhấn để xem chi tiết</summary>

### Cách sử dụng

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

## Tên miền và chứng chỉ

Mọi thứ liên quan tới TLS nằm ở tab **Tên miền và chứng chỉ** trong Cài đặt bảng điều
khiển. Bảng điều khiển và dịch vụ subscription mỗi bên chọn tên miền riêng, còn đường
dẫn chứng chỉ đi theo tên miền bạn chọn — không phải chép tay đường dẫn tệp nữa.

### 🔐 Chứng chỉ tự động (ACME / Let's Encrypt) — Khuyến nghị

Nhập tên miền, thêm email và bấm cấp phát: 2S-UI sẽ lấy về và tự động gia hạn chứng chỉ
Let's Encrypt miễn phí. Việc cấp phát chạy qua **acme.sh** — ở lần dùng đầu tiên, bảng
điều khiển sẽ tự cài acme.sh cho bạn (cùng với `socat`, cần cho xác thực standalone) và
bật gia hạn tự động bằng chính cron job của acme.sh, nên bạn không phải tự lập lịch gì
cả. Sau khi hoàn tất, bảng điều khiển có thể truy cập tại `https://<your-domain>:2095/app`.

Phương thức xác thực mặc định là **auto** — dùng standalone khi cổng 80 còn trống, ngược
lại mượn nginx đang chạy và tự tạo một khối `server_name` tối thiểu dưới
`/etc/nginx/conf.d` nếu chưa có. Bạn có thể chỉ định rõ **standalone** hoặc **nginx** nếu
muốn tự quyết. Khi gia hạn, chứng chỉ được nạp nóng; không cần khởi động lại.

> Yêu cầu cổng TCP **80** có thể truy cập từ internet (HTTP-01 challenge). Để
> publish cổng 80 với Docker: bỏ ghi chú dòng `80:80` trong `docker-compose.yml`,
> hoặc thêm `-p 80:80` vào `docker run`. Chứng chỉ được lưu trong `/root/cert/<tên-miền>/`
> với tên tệp `fullchain.pem` / `privkey.pem` và vẫn tồn tại sau khi khởi động lại (volume
> trong lệnh Docker ở trên chính là để map đường dẫn này ra ngoài). Nếu tên miền/cổng được
> cấu hình sai, 2S-UI sẽ chuyển về HTTP.
> ACME chỉ hỗ trợ Linux và bị ẩn trên Windows.

### Dùng chứng chỉ của riêng bạn

Chứng chỉ do bạn tự quản lý — Cloudflare origin CA, CA nội bộ, hay kết quả từ certbot —
có thể được đăng ký ngay trong tab đó. 2S-UI kiểm tra tệp có đọc được không, khóa có khớp
với chứng chỉ không, và chứng chỉ có thực sự bao phủ tên miền không; sau đó tên miền này
có thể chọn được ở tab Giao diện và Subscription như mọi tên miền khác. Chứng chỉ đã đăng
ký được đưa vào các bản sao lưu cơ sở dữ liệu.

### Đứng sau reverse proxy

Bật **TLS được kết thúc bởi reverse proxy** và 2S-UI sẽ viết vhost giúp bạn:
`/etc/nginx/conf.d/s-ui-proxy-<tên-miền>.conf`, trỏ về bảng điều khiển kèm đúng các header
chuyển tiếp, kiểm tra bằng `nginx -t`, reload, và khôi phục lại trạng thái cũ kèm chính
thông báo lỗi của nginx nếu có bước nào thất bại. Máy chủ subscription có thể đứng sau
cùng một proxy.

<details>
  <summary>Bạn muốn tự cấp phát chứng chỉ? (Certbot)</summary>

### Certbot

```bash
snap install core; snap refresh core
snap install --classic certbot
ln -s /snap/bin/certbot /usr/bin/certbot

certbot certonly --standalone --register-unsafely-without-email --non-interactive --agree-tos -d <Your Domain Name>
```

Sau đó đăng ký `fullchain.pem` / `privkey.pem` thu được vào tab **Tên miền và chứng chỉ**.

</details>

## Số lượng Stargazers theo thời gian
[![Star History Chart](https://api.star-history.com/svg?repos=shenaba/2s-ui&type=Date)](https://star-history.com/#shenaba/2s-ui&Date)
