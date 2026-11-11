# PROXY protocol 实测步骤（nginx stream 前置）

用真实的 nginx stream 前置验证 inbound 的 PROXY protocol 支持（#260、#266）。准备一台带公网 IP 的测试机，大约半小时。示例里的域名、端口都可以换成自己的。

## 准备

- **测试机**：装好 nginx，并确认带 stream 模块：`nginx -V 2>&1 | grep -o with-stream`。
- **面板**：用待测版本的二进制替换 `/usr/local/s-ui/sui`，然后 `systemctl restart s-ui`。
- **客户端**：一台能连代理的机器。先在**不走代理**时运行 `curl ifconfig.me`，记下它的公网 IP，后面要对照。
- **日志**：服务端开一个终端一直看着：`journalctl -u s-ui -f`。

## 一、配置

### 1. 面板里建 inbound

vless + TLS 或 REALITY 都可以：

- 监听 `127.0.0.1`，端口 `10443`
- 打开监听设置里的 **PROXY protocol** 开关
- 客户端设置 › 多域名：添加一行 `proxy.example.com:443`，否则订阅链接会用 10443
- 建一个测试用户

### 2. nginx stream 按 SNI 分流

加到 `nginx.conf` 顶层，与 `http {}` 并列：

```nginx
stream {
    map $ssl_preread_server_name $backend {
        proxy.example.com  vless;   # REALITY 就填 inbound 里的伪装 SNI
        default            web;
    }
    upstream vless { server 127.0.0.1:10443; }
    upstream web   { server 127.0.0.1:8443; }

    server {
        listen 443;
        listen [::]:443;
        ssl_preread on;
        proxy_pass $backend;
        proxy_protocol on;
    }
}
```

`proxy_protocol on` 对两个上游都生效，所以网站那边也要改成接收 PROXY 头：

```nginx
# http {} 里的网站 server
listen 127.0.0.1:8443 ssl proxy_protocol;
set_real_ip_from 127.0.0.1;
real_ip_header proxy_protocol;
```

改完运行 `nginx -t && nginx -s reload`。

## 二、测试项

### T1 记录的是真实 IP（核心功能）

客户端连上代理随便访问几个网站，面板在线列表里这个用户的来源应该是客户端的公网 IP，而不是 `127.0.0.1`。

### T2 网站不受影响

访问 `https://你的网站域名`，页面正常，nginx 访问日志里记录的是真实 IP。

### T3 前置忘了发 PROXY 头时会被拒

注释掉 `proxy_protocol on;`，reload nginx，客户端应该连不上。服务端日志出现 `no PROXY header from a trusted front`。测完改回来。

### T4 端口探测不刷 Error 日志

在服务端连跑几次 `nc -zv 127.0.0.1 10443`，日志里不应出现 Error 级别的记录。

### T5 单用户 IP 限制按真实 IP 生效

给测试用户设 IP 限制为 1，用两个不同网络（比如家里宽带和手机热点）同时连接，第二个应被拒绝。

修复前两边都会被记成 `127.0.0.1`，这一项测不出来。

### T6 经直连伪造 PROXY 头会被拒（安全）

在客户端经代理的本地 socks 端口（假设是 1080）运行：

```bash
curl -v -x socks5h://127.0.0.1:1080 --haproxy-protocol --haproxy-clientip 6.6.6.6 http://127.0.0.1:10443/
```

- `--haproxy-clientip` 需要 curl 8.2 及以上；版本低就去掉它，只用 `--haproxy-protocol`。
- 预期：连接在拨号时就被拒绝（curl 报 SOCKS 连接失败），服务端日志出现 `refused to dial 127.0.0.1:10443: a PROXY-enabled inbound on this host`，在线列表里始终没有 `6.6.6.6`。
- 用 `localhost:10443` 代替 `127.0.0.1:10443` 再试一次，结果应相同。
- 测试期间前端（nginx）转来的正常连接不受影响：T6 反复跑几百次，T1 的客户端照常可用。
- 如果路由规则本来就拦截了内网目标，请求会更早失败，这一项就测不到伪造头的检查。可以先临时放开这条规则。

### T7 经本机中转伪造会被拒（如果有 WARP 或本地 socks outbound）

把测试用户路由到那个 outbound，再跑一次 T6 的 curl。

- 预期：日志出现 `refused to relay to 127.0.0.1:10443`。
- 同时经这个 outbound 正常上网不受影响。

### T8 速度和 vision splice 正常

如果用的是 vless + `xtls-rprx-vision`，测一次速，对比开 PROXY 之前，速度和服务端 CPU 占用应该差不多。

### T9 关掉开关后恢复原状

关掉 inbound 的 PROXY protocol 开关，nginx 也去掉 `proxy_protocol on`，一切回到改动前的样子。

## 三、Docker 部署

- 面板在容器里时，inbound 监听保持 `::`，端口只映射到宿主机回环：`-p 127.0.0.1:10443:10443`。
- 宿主机 nginx 的上游写 `127.0.0.1:10443`。
- 设置 › 界面 › **可信 PROXY 前置** 填 Docker 网关，通常是 `172.17.0.1`，以 `ip -4 addr show docker0` 的输出为准。
- 然后跑一遍 T1、T3、T6。
- 不要把端口映射到所有网卡（`-p 10443:10443`）：容器里的客户端可以经宿主机公网地址绕回来，从可信的 Docker 网关进入，伪造的 PROXY 头就会被采信。表单里的提示也写了这一点。

## 结论

T1、T3、T5、T6 都符合预期，就说明核心功能和安全修复都验证过了。

## 已知限制

以下情况代码层面检查不到，详见 `core/proxyheader` 的包注释：

- 改端口的 Docker 映射（如 `8443:10443`），经本机中转用域名访问时；
- 端口映射到所有网卡时的 Docker 回环 NAT；
- 本机上指向 PROXY inbound 的 iptables REDIRECT/DNAT；
- 客户端自己直连本机上无鉴权的中转（WARP 的 socks 端口、本地 gost 等），再用中转自己的协议让它连回 PROXY inbound：core 只看到一次到本机其他端口的拨号。给这类中转加鉴权，或用路由规则拒绝客户端访问本机和私网地址（`ip_is_private`）；
- ws、http、httpupgrade、grpc lite 传输优先使用客户端可伪造的 `X-Forwarded-For`（#279，main 上已存在）。
- 自建 NAT64/SIIT 翻译器用的 IPv6 前缀（非 `64:ff9b::/96`、`64:ff9b:1::/48`）无法识别，不要把它填成可信前置，否则它背后的所有 IPv4 客户端都会被信任。

另外：前置从本机另一个地址连进来（如 nginx 用 `proxy_bind` 指定了别的本机地址）时，需要把那个地址填进可信前置，本机地址不会自动全部信任。
