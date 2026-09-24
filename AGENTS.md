# AGENTS.md

This file provides guidance to Codex (Codex.ai/code) when working with code in this repository.

## What this is

2S-UI is a sing-box web panel — a maintained fork of `alireza0/s-ui`. Module path: `github.com/shenaba/2s-ui`.

Two facts shape almost everything else:

1. **sing-box is embedded as a Go library, not supervised as a subprocess.** `core/` wraps it directly (`core.Box` reimplements sing-box's own `box.Box` so the panel can inject its own trackers). Restarting "the core" is an in-process object teardown, not a process kill.
2. **The frontend compiles into the Go binary.** `frontend/dist/` → `web/html/` → `//go:embed *` in [web/web.go](web/web.go). There is one deployable artifact: `sui`.

## Commands

Go toolchain per `go.mod`: **1.26.7** (`CONTRIBUTING.md` says 1.25 — go.mod is authoritative; CI uses `go-version-file: go.mod`). CGO is required for the SQLite driver.

```bash
./build.sh          # frontend (npm i && npm run build) -> web/html -> go build -o sui
./runSUI.sh         # build.sh, then SUI_DB_FOLDER="db" SUI_DEBUG=true ./sui
```

Panel: http://localhost:2095/app/ (`admin`/`admin`). Sub server: :2096.

### Reproducing the PR gate locally

`ci.yml` is the only thing that runs on a PR, and it is exactly these commands:

```bash
export BUILD_TAGS=with_quic,with_grpc,with_utls,with_acme,with_gvisor,badlinkname,tfogo_checklinkname0,with_tailscale,with_cloudflared,with_openconnect,with_openvpn,with_usbip
go mod download github.com/sagernet/sing-box && bash scripts/check-protocol-copies.sh
go vet -tags "$BUILD_TAGS" ./...
go build -ldflags="-w -s -checklinkname=0" -tags "$BUILD_TAGS" -o /dev/null .
go test -tags "$BUILD_TAGS" -ldflags="-checklinkname=0" ./...
cd frontend && npm ci && npm run build   # = vue-tsc --noEmit && vite build; type errors fail the build
```

`-checklinkname=0` is **required, not cosmetic**: sing-box's `badtls` references unexported `crypto/tls` internals, and the link otherwise fails with `invalid reference to crypto/tls.(*Conn).handlePostHandshakeMessage`.

The Go build does not need the frontend — `web/html` is gitignored and the `//go:embed *` pattern matches `web.go` itself, so it resolves even with no frontend built.

**`npm ci` is not what ships — the PR gate is the only thing that uses it.** It installs the lockfile verbatim and never re-resolves the tree, so it does not enforce peer ranges. Every path that actually produces a binary (`release.yml`, `windows.yml`, `Dockerfile`, `build.sh`, `windows/build-windows.ps1`) runs `npm install`, which *does* re-resolve. **A dependency bump can therefore be green on CI and still break every shipping path with `ERESOLVE`.** That is exactly how pinia 4 reached main: `vue-router` declared `peerOptional pinia@^3.0.4`, `npm ci` never looked, and `release.yml` died in 13s on the merge commit. Before trusting CI on a dependency PR:

```bash
cd frontend && npm install --package-lock-only   # re-resolves; catches what npm ci hides
```

### Frontend

```bash
cd frontend
npm run dev      # vite --host on :3000 (frontend/CONTRIBUTING.md's ":5173" is wrong)
npm run lint     # eslint . --fix
```

`npm run dev` proxies `/app/api` → `localhost:2095` and `/app/ws` (with `ws: true`) to the same place. In DEV only, a custom axios adapter falls back to `src/plugins/mock.ts` when the backend is down (the vite proxy turns `ECONNREFUSED` into a 502, which the adapter reads as "not running").

**That fallback no longer covers the live data.** Since the polling→WebSocket switch, `load`/`status`/`stats` arrive over a native socket that never touches axios, so `mock.ts`'s `api/load` and `api/stats` entries are dead for the live path. **Without the Go side running the dashboard renders empty** — only the one-shot `api/status?r=sys` and `api/changes` still hit the mock. The config views no longer hang on it (`Data().waitReady()` gives up after 15s and logs to the console), but you get an empty panel, not fake data. Start the backend.

### Tests

**CI runs every Go test there is** (`go test ./...`, added in #96 — before that they existed but nothing invoked them). The table below is the map, not a census; `git ls-files '*_test.go'` is the count, and it moves.

| File | Covers |
|---|---|
| `util/outJson_test.go` | `TestStripServerTlsFields` — server-only TLS fields stripped from client configs (issue #51) |
| `util/shadowsocks_test.go` | method → client-config key selection (`ShadowsocksClientConfigKey`) |
| `util/genLink_test.go` | naive link schemes per network, userinfo escaping, per-transport remarks, IPv6 bracketing per output shape |
| `util/host_test.go` | `NormalizeHost`/`HostForURI` — which output shape wants an IPv6 literal bracketed |
| `util/totp_test.go` | the RFC 6238 vectors, drift window, and the counter-underflow guard |
| `cmd/migration/version_test.go` | `TestVersionBefore` — the version comparison that gates migrations |
| `service/acme_test.go` | the nginx-facing decisions in `EnsureVhost`/`SyncVhosts`/`ensureNginxServerBlock` |
| `service/iplimit_test.go`, `core/tracker_conn_test.go` | per-client IP admission/eviction, and the connection tracker under it |
| `service/loginguard_test.go` | the login limiter's policy: `planLoginFailure`, ban arithmetic, key folding |
| `service/client_test.go`, `service/hub_test.go`, `service/nodeSync_test.go` | client diffing, hub notify, node reconcile; also which expiry threshold a warning names |
| `api/utils_test.go`, `sub/jsonService_test.go`, `sub/linkService_test.go` | request helpers; subscription JSON assembly; share links with their remarks kept |
| `service/notify/*_test.go` | the bus (fan-out, per-subscriber isolation, panic containment), every suppression rule, six-language message coverage and placeholder parity, and that the SMTP sender gives up rather than hanging |
| `service/tgbot/*_test.go` | role resolution (the whole authorisation surface), what a stranger may learn, session stores under concurrency, the per-admin command menus, and the Telegram-id binding conflict |
| `service/digest_test.go`, `service/notifySettings_test.go`, `database/backup_test.go` | the status/alert digests; that every notify\* setting has a default (a missing one is silently unstorable); the backup round-trip |

`service/acme_test.go` is deliberately all pure functions: every one of those decisions was factored out so it can be verified on a machine with no nginx. Keep it that way — the fs/exec halves (`precheckVhost`, `CheckVhosts`) have no coverage precisely because they cannot get any here.

**There is still no frontend test runner** — `frontend/package.json` has dev/build/preview/lint and nothing else, so `vue-tsc --noEmit` plus `vite build` is the whole frontend gate. Behaviour in `src/types/*.ts` and in `.vue` logic is unverified by anything automatic. For a pure-logic module you can bundle it standalone with the project's own rolldown (vite 8 ships rolldown, **not** esbuild) and drive it from node; that is throwaway verification, not coverage, and should be described as such.

Everything else is verified the same way it always was: build, run, exercise the changed area by hand (both themes, mobile ≤820px, `fa` RTL for UI work). CI green now means it compiles **and** the Go tests pass — it still says nothing about the frontend beyond types.

### Build tags

Release tags: the CI set above, plus `with_naive_outbound,with_musl` on platforms where cronet ships. `core/register_*.go` / `register_*_stub.go` pair every optional protocol with a stub behind `//go:build !tag`, so **dropping `with_naive_outbound` and/or `with_tailscale` still compiles and runs** — those protocols just return "rebuild with -tags ..." at runtime. Useful when the cronet/Chromium toolchain isn't available locally.

Note the tag sets legitimately differ per target: `Dockerfile` uses `with_purego` and omits `badlinkname`/`-checklinkname=0`; `windows/build-windows.ps1` is narrower than `windows.yml`, so a local Windows build is **not** feature-equivalent to the released one.

## Architecture

### Package map — only the names that mislead

The rest (`api/`, `service/`, `util/`, `cmd/`, `cronjob/`, `logger/`, `middleware/`, `windows/`) mean what they say; `ls` is enough. These don't:

| Path | What it actually is |
|---|---|
| `config/` | **No sing-box config here.** Only `//go:embed`ed version/name, `SUI_*` env vars, and DB/cert path resolution. The real config is assembled from the DB at runtime (below). |
| `database/` vs `db/` | `database/` is code (GORM + `model/`). `db/` is the runtime SQLite data dir (gitignored). |
| `sub/` | Not a subpackage of `web/` — an independent second gin server on :2096 with its own listen/port/domain/cert settings. |
| `network/` | Not protocol code — ACME issuance, TLS config, and a listener that sniffs the first packet: plaintext HTTP hitting the HTTPS port gets a 307 to `https://`, anything else falls through to the TLS handshake. |
| `app/` | One file. Lifecycle orchestration only (DB → settings → core → cron → both servers). |
| `core/` | The sing-box wrapper. Business logic lives in `service/`. |
| `core/protocol/` | **Verbatim copies of sing-box's own inbound implementations**, forked only so `UpdateUsers` can be attached (see below). Not a place to put new code. |
| `service/notify/` | Not a Telegram integration — an in-process event bus with a suppressor in front of it, and three interchangeable subscribers (Telegram, webhook, SMTP) each on its own queue and worker. Everything it knows is in memory on purpose (see `Suppressor`), which is why event sources whose condition repeats every few seconds have to debounce themselves. `Publish` never blocks and never returns an error. |
| `service/tgbot/` | The interactive half, and it sits **above** `service`, not inside it — every write goes through `ConfigService.Save`, the same path the HTTP API uses. It cannot be called from `service` without closing an import cycle, which is why it polls the settings for its own credentials instead of being reloaded. |

### The config assembly loop (the central mechanism)

The sing-box config is **not a file**. It is assembled per start from the database:

```
settings.config (base JSON: log/dns/ntp/route/experimental)
  + inbounds/outbounds/services/endpoints tables (each row -> one JSON object)
  = SingBoxConfig -> json.Marshal -> core.Start(rawConfig)
```

This lives in `ConfigService.GetConfig` ([service/config.go](service/config.go)). Consequences worth internalizing:

- Editing an entity means editing a **DB row**, then restarting the core. `ConfigService.Save` is the single write path: it opens a transaction, dispatches by object type, writes a `Changes` audit row, and bumps `LastUpdate` (which drives the frontend's `api/changes` poll).
- `StartCore` is guarded by `startCoreMu` + a 15s `startCooldown` after a failure, and a `@every 5s` cron job (`checkCoreJob`) restarts the core if it's down. A failed config does not permanently wedge the panel, but it also means **your bad config will be retried on a loop** — read the log, don't just re-save.

### `core/protocol/` — forked sing-box inbounds, and what it costs

Editing a client used to go through `InboundService.RestartInbounds`, which removes the inbound from the core and adds it back. For the QUIC protocols that also destroys the QUIC session, so **changing one user dropped every connected user on that inbound**.

sing-box keeps an inbound's user table in unexported fields and its `Inbound` types do not surface `UpdateUsers`. The underlying services (`*vless.Service`, …) do have it, but it is unreachable from outside the sing-box package. So seven inbounds (anytls, hysteria, hysteria2, trojan, tuic, vless, vmess) are **copied verbatim** into `core/protocol/`, `core/register.go` registers the copies, and `Core.UpdateInboundUsers` dispatches on the option type. Shadowsocks is not copied — sing-box already exports `MultiInbound.UpdateUsers`. Anything unrecognised returns `handled=false` and the caller falls back to the full restart; certificate changes (`service/tls.go`) keep restarting on purpose.

Two things to internalise:

- **The copies go stale silently.** They keep compiling after a sing-box bump while running the old implementation. `scripts/check-protocol-copies.sh` diffs them against the version in `go.mod` and runs in CI, so **run it after every sing-box bump** and re-copy from the module cache rather than patching around it. It does not necessarily fail: these files change rarely upstream, and 1.13.15 → 1.13.18 came through with 7/7 ok. The header comment on each copy deliberately carries no version number — it went stale twice — since the script derives the real one from `go.mod`.
- **The copies key their service by user name, not by list position** (`Service[string]`, not sing-box's `Service[int]`) — ported from upstream #1231. sing-box never rewrites a user list in place, so a position is a stable identity there; `UpdateUsers` does rewrite it, under live sessions, and a position is not: deleting a user shifted every later one, which mis-attributed traffic and could index past the end of the name slice outright. This is now the only deliberate divergence from sing-box in those six files, and `scripts/check-protocol-copies.sh` encodes its exact line count per protocol so a bump that touches anything else still shows up. **Consequence: the client name is load-bearing.** An empty or duplicate name makes two clients one identity — trojan's `UpdateUsers` rejects the repeat and fails the whole update, vmess keeps only the last, vless mis-attributes flow — which is why `normalizeClientName` rejects empty names and trims, on every save path including a cluster push.

Who gets disconnected is decided by `ConnTracker.CloseConnByInboundUsers`, matching on **user name**. Consequence: rotating a client's UUID or password does not drop its live connection.

### Model pattern: fixed columns + `Options` blob

`database/model/` entities (`Inbound`, `Outbound`, `Service`, `Endpoint`) promote only the fields the panel needs into real columns (`Id`, `Type`, `Tag`, `TlsId`, `Addrs`, `OutJson`) and stuff **everything else** into an `Options json.RawMessage`. Custom `UnmarshalJSON` splits incoming JSON that way; then there are two marshalling shapes:

- `MarshalJSON` → **sing-box shape** (type/tag/tls + spread `Options`) — what gets fed to the core.
- `MarshalFull` → **panel shape** (adds `id`, `tls_id`, `addrs`, `out_json`) — what the API returns to the UI.

So a new sing-box protocol field usually needs **no Go change** — it rides along in `Options`. It needs a frontend type in `frontend/src/types/` and a form component. That's why those types are snake_case: they serialize straight to sing-box JSON.

### Process shape

[app/app.go](app/app.go) `APP.Init/Start/Stop` wires and owns everything: DB → settings → `core.Core` → cronjobs → **two independent gin servers**:

- `web.Server` (:2095) — panel SPA + `api` + `apiv2`.
- `sub.Server` (:2096) — subscription delivery only, `GET/HEAD /:subid`.

Both are separately configurable (listen/port/domain/cert) and each independently supports ACME. `main.go` runs the panel only when `len(os.Args) < 2`; otherwise it dispatches to `cmd.ParseCmd()`. `SIGHUP` triggers `RestartApp()`.

### The two APIs share one service

`api/apiHandler.go` (v1) and `api/apiV2Handler.go` (v2) are **action-dispatch switches over a shared `ApiService`**, not REST. Both route `POST /:action` + `GET /:action`.

- **v1** = session cookie (`gin-contrib/sessions`), used by the SPA.
- **v2** = `Token` header, checked against an in-memory token slice; mutations via v1 (`addToken`/`deleteToken`) must call `apiv2.ReloadTokens()`.

`Save` takes a `fanout` argument that decides whether a client/inbound change is pushed to managed nodes right away. v1 passes `true`; v2 passes it only when the request carries `sync` (issue #94, added in #99), because a node that has this panel as its own master would otherwise bounce the pushed change back. Without `sync` a change still converges — through the hourly reconcile, up to an hour later. A malformed `sync` value is rejected rather than quietly treated as "no fanout".

Adding an endpoint = adding a `case` to the switch **and** a method on `ApiService`. Response convention throughout is `{success, msg, obj}` (`Msg` in `frontend/src/plugins/httputil.ts`).

There is **no per-entity endpoint**: every mutation is `POST api/save` with `{object, action, data}`. On the frontend, that's `Data().save(object, action, data)` in `store/modules/data.ts`.

**A save answers with what it wrote, not with the new panel state** (`ApiService.savedRows`). It used to reply by running the read endpoint over every table the change had touched — `ConfigService.Save` returned a `[]string` of tables to reload, and `Save` fed that to `LoadPartialData` — so creating one client returned every client, every inbound, and a `clientsSeq`: a payload shaped by what the SPA needed to repaint, paid for by every API caller (issue #158). Now `ConfigService.Save` returns a `SaveResult{Object, Action, Ids}` describing the write, and a **single-row** client write (`new`/`edit`) additionally carries that row in **full** (`getById`, so `config` and `links` are there — links are generated server-side and a caller cannot rebuild them). `Ids` covers the **primary object only**; a tls edit also rewrites client links and nothing reports those.

Two consequences worth keeping:

- **Bulk and delete actions answer with ids, not rows.** `editbulk` receives every client the drawer had selected, and whole rows carry `config` and every generated link — echoing them back would resend the client table with its credentials, *larger* than the payload this change removed, to a panel that reloads and never reads the reply. Rows are one `getById` away for a caller that wants them. Relatedly, an empty read is normalised to `[]`: the row can be deleted between the commit and the read, and a nil slice marshals as `null`, so the field's type would depend on a race.
- **Only clients report ids.** The other services' `Save` methods return bare `error` through dozens of sites, so plumbing ids through them is its own piece of work. `Ids` is `omitempty` — absent rather than empty — so adding a second object later is additive.
- **The SPA refreshes itself**: `Data().save()` awaits `loadData()` (`GET api/load`) rather than applying the reply. That is not a leftover round trip that a smarter client-side merge could remove — the inbounds list shows a per-inbound user count that is a **join computed per read** (`InboundService.GetAll`), so no merge of the returned rows can keep it honest. Awaited, not left to the websocket: the 200ms `configDebounce` push is too late for a closing drawer, and a closed socket would mean never.

### The BASE_URL loop

The panel path is a **runtime DB setting**, not a build constant. This spans five files and is easy to break:

1. `web.go` reads `GetWebPath()` and renders the built `index.html` **as a Go template** with `{"BASE_URL": base_url}`.
2. `frontend/index.html` ships `window.BASE_URL = "{{ .BASE_URL }}"`, falling back to `/app/` if the first char is `{` (i.e. unrendered → dev mode). Vite doesn't touch inline script bodies, so the Go template survives the bundle.
3. `vite.config.mts` sets `base: ''` (relative asset URLs), `router` uses `createWebHistory(window.BASE_URL)`, and `plugins/api.ts` sets `axios.defaults.baseURL = "./"`.

**Never hardcode `/app/` and never make axios paths absolute** — it breaks both custom web paths and dev mode.

Related: `web.go` serves `assets/` with `Cache-Control: max-age=31536000`, so `vite.config.mts` names every emitted file `[name]-[hash]` — **content** hashes, not the per-build id it used before 1.6.2. An unchanged chunk therefore keeps its name across an upgrade and stays cached; only changed ones are refetched. `[name]` stays in front so split chunks remain tellable apart, and `assetFileNames` covers the CSS chunks and `@fontsource` woff2 files, which used to land under bare upstream names and so carried a year-long max-age they could never invalidate. `public/assets/` (the two favicons) bypasses this and keeps the literal names `index.html` hardcodes.

### Auth

`meta.requiresAuth` in the router is declared but **never read**. Enforcement is server-side on full page load (`web.go`'s `NoRoute` redirects to `login`), plus a reactive client-side catch: any `{success:false, msg:"Invalid login"}` response triggers logout in `httputil.ts`. Session is cookie-based; no token is stored client-side.

Three things guard the login itself, and each has a detail that is easy to undo:

- **Rate limit** (`service/loginguard.go`). Five failures inside five minutes ban an identity for fifteen, counted against **both** the source IP and the username so spreading across either axis does not buy a fresh budget. State is a DB row per identity (`model.LoginAttempt`), not memory, because a restart must not clear a ban — and a restart is not rare here. The identity comes from `getRemoteIp`, which **only honours forwarding headers when `webNginx` is set**: they are client-supplied, so on a directly exposed panel trusting them would hand every attempt a fresh identity and defeat the limiter entirely. Which entry it takes matters just as much: it reads the **last** `X-Forwarded-For` entry, because `$proxy_add_x_forwarded_for` (what the generated vhost sets) *appends* to the client's own header and so leaves a forged value first, while a replacing proxy leaves a single entry — last is right under both. **`X-Real-IP` is deliberately ignored** even though the generated vhost sets it: nginx forwards headers it was not told to overwrite, so behind a proxy that only configures `X-Forwarded-For` it would be pure client input. The address is then folded the same way the per-client IP limit folds it (`normalizeLoginIP` mirrors `core`'s `normalizeSrc`: unmap v4-in-v6, mask v6 to `core.IPv6IdentityPrefixBits`) — without that, one host rotating through its own /64 spends a fresh failure budget per address. `BanRemaining` is checked **before** the password so a banned attempt costs one indexed lookup instead of a bcrypt comparison; the consequence is that the n-th failure is itself still answered as "wrong password" and the ban only shows from the next attempt on. All three settings default in `defaultValueMap`, and **any of them at zero disables the limiter** (a zero window or ban would refuse nobody).
- **TOTP 2FA** (`util/totp.go`, `User.TwoFaSecret`). Hand-rolled RFC 6238 — SHA-1/6 digits/30s, ±1 step — rather than a dependency; `util/totp_test.go` pins it to the RFC's own vectors, which is what says an authenticator app will interop. `ErrTwoFaRequired` is returned **only after the password matched**, so the "enter a code" prompt cannot be used to enumerate which accounts have 2FA; a wrong code returns the same message as a wrong password. That branch **counts against the rate limit** even though nothing was wrong with the request: it has already paid for the bcrypt comparison, and a leaked password would otherwise buy unlimited cheap CPU burn. A genuine two-step login hits it once and then clears the tally by succeeding. Enrolment stores nothing until a code from the candidate secret verifies (`twoFaSetup` → `twoFaEnable`), so an abandoned enrolment cannot lock anyone out; **`twoFaEnable` refuses to overwrite a live secret** — replacing one goes through `twoFaDisable`, which asks for the password, and skipping that check would make enrolment a way around it. `sui admin -disable-2fa` is the recovery path when the second factor itself is what is unavailable (lost authenticator, or a clock that moved backwards past the replay high-water mark). Authentication goes through `ValidateTOTPAfter`, which refuses any counter at or below `User.TwoFaCounter` and burns the matched one *before* granting the login — a code is valid for up to 90 seconds, so without that it would work as a static password for the rest of its window. The secret leaves the panel exactly once, during enrolment: `User.TwoFaSecret` is `json:"-"` and `GetUsers` selects a computed `twoFa` boolean instead.
- **Credential fingerprint** (`service.CredentialFingerprint`, `api/session.go`). The session is a signed cookie with no server-side state, so changing the password cannot by itself invalidate the ones already issued. Every session carries a digest of `username + password hash`, re-checked in `GetLoginUser` — which means every credential write retires the old sessions for free, with no invalidation call to remember. **It reads the row on every authenticated request, and that is deliberate**: an in-process cache cannot be kept correct here, because `sui admin -reset` writes the row from a *separate process* and `ImportDB` swaps the whole database file underneath a running panel, so both would leave sessions alive that the change was meant to retire. One indexed lookup per request is the price. This is also what closes the websocket, whose handshake is the only place it authenticates. An empty fingerprint means "vouch for nothing" and is rejected rather than compared, which is why sessions predating the field require one fresh login after an upgrade.

### Stats & clients

`core.StatsTracker`/`ConnTracker` are appended to sing-box's router as trackers; a `@every 10s` cron job flushes counters into the `stats` table, bucketed by a unique index on `(resource, tag, date_time, direction)`. Clients hold `Inbounds` as a JSON id array — the "multi-inbound per user" model the project is built around — and editing a client diffs the inbound set and hot-restarts only affected inbounds (`InboundService.RestartInbounds`).

**A client's `up`/`down` is its total across every inbound it belongs to**, so summing an inbound's clients does not give that inbound's traffic — under the multi-inbound model it gives roughly the same number for every inbound sharing a busy client, which is exactly what the inbounds list used to show. The per-inbound figure only exists in the `stats` table under `resource='inbound'`, and aggregating that per read is not affordable (one row per active tag/bucket/direction over the whole retention window, re-scanned every 10s). So `service/stats.go` keeps the totals in memory — seeded once from that table, advanced by each flush's own deltas, published on the **live** payload next to `onlines`/`ipCounts` (the config half would freeze it until the next save). Anything that replaces the stats table wholesale must call `InvalidateInboundTraffic`.

### Nodes (the cluster model)

A panel can manage other panels as **nodes** — `service/node.go` + `service/nodeSync.go`, `model.Node`, `frontend/src/views/Nodes.vue`. Three cron jobs drive it, all no-ops with zero nodes: `@every 5s` `NodesJob` (probe, then reconcile whatever is marked dirty), `@every 1m` `NodeTrafficJob` (pull each node's counters and fold the delta into the master's per-client totals), `@every 1h` `NodeReconcileJob` (drift safety net over every online node).

Four things here are load-bearing and easy to undo by accident:

- **Quota is deliberately not replicated.** Pushed clients get `volume: 0` with `expiry` copied verbatim ([service/nodeSync.go](service/nodeSync.go) `expectedClients`). Each node counts traffic locally, so copying a 100 GB quota to three nodes would hand out 300 GB. Enforcement is central instead: collect, judge the cluster-wide total, push `enable=false`. A node panel showing `∞` for a limited client is correct, not a sync failure.
- **Adopting a node inbound stores a local replica row**, and adoption drops `tls_id` (TLS terminates on the node). The replica reaches subscriptions by exactly one path: `refreshNodeLinks` regenerates a share link and folds it into `client.Links` as a `[node] ` external entry. The outbound generators must keep excluding replicas (`node_id IS NULL`) — building one directly from the replica row too produces a duplicate tag, and sing-box/mihomo reject the whole config on that, so the subscription cannot be imported at all (#95).
- **Node identity is the client name.** `expectedClients` and `actualClusterClients` both map by it, which is why duplicate client names are rejected at the API rather than only in the SPA.
- **Reconcile compares, it does not blindly push.** `clientDiffers` decides what moves; a field it does not compare cannot be repaired if the node side changes it. `volume` is one of those — a quota set on the node would stick.

### Version & migrations

`config/version` is `//go:embed`ed — **not** injected via `-X` ldflags. Bumping a release means editing that file in-tree; the git tag and the embedded version can drift. `cmd/migration/` gates on the **2s-ui** version line, not upstream's (users' `dbVersion` tracks 2s-ui releases) — see the comment in `cmd/migration/main.go` before adding one.

### `sui` CLI

`sui -v` · `admin` (`-show/-reset/-username/-password/-disable-2fa`) · `uri` · `migrate` · `setting` (`-port/-path/-subPort/-subPath`) · `backup` (`-output`, `-exclude=changes,stats`). All except `-v` need a resolvable DB path (`SUI_DB_FOLDER`, else relative to `argv[0]`).

## Frontend conventions

[frontend/DESIGN_SYSTEM.md](frontend/DESIGN_SYSTEM.md) (in Chinese) is authoritative and worth reading before UI work. The load-bearing rules:

- **No UI component library, by design.** ~34 hand-written primitives in `src/components/ui/`. Don't propose adding one (headless included). ECharts is fine — a chart renderer isn't a component library.
- **Tokens only** — `var(--brand)`, `var(--surface-3)`, `var(--text-2)`, `var(--line)`; never hardcode colors. Semantic colors come from `@/plugins/colors.ts`.
- **Theming** = `<html data-theme="dark|light">`. The pre-paint script in `index.html` duplicates the `2sui-theme` localStorage read in `store/modules/app.ts` — change one, change both.
- **No global component registration**; always import explicitly.
- **Never use native `<select>`** — use `<Select>` (it parses slot vnodes so bound values keep their type).
- **Overlays must use `pushOverlay()`** from `components/ui/overlay.ts`; never hand-roll an Esc listener.
- Drawers/modals live in `layouts/drawers/`, not `components/`. Forms are under `components/forms/in|out/`, but since 1.6.2 that split is no longer by direction: the shared ones (`Dial`, `Listen`, `Multiplex`, `Transport`, `Headers`, `OutTLS`) all live in `out/` and are imported by the **inbound** and **DNS** drawers too. Look there before adding a second copy under `in/`.
- After touching `components/ui/`, `npx vue-tsc --noEmit` must be 0 errors; `scripts/extract-component-api.mjs` regenerates the doc's prop tables.

i18n keys are **structural, not just display**: route `name`s are i18n keys, and toasts are built as `t('actions.'+action) + ' ' + t('objects.'+obj)` — renaming a key silently breaks them. File names ≠ locale keys (`zhcn.ts` → `zhHans`, `zhtw.ts` → `zhHant`). `locales/ui/*.ts` are nominally generated, but the generator needs an external design-handoff dir that isn't in this repo — **treat them as source**.

ESLint is deliberately loosened (`vue/no-mutating-props` is `shallowOnly` because the forms pattern is parent-passes-object/child-mutates-fields; `eqeqeq` off; several rules downgraded to `warn`). **A clean lint run still emits warnings — that's expected.**

## Release quirks

- `release.yml` and `windows.yml` trigger on **both** `push: main` and `release: published` — merging a PR then publishing a release runs the full 7-platform matrix **twice**. Only the `release` run uploads assets (the upload step is gated on `github.event_name == 'release'`). Both carry a `concurrency:` group keyed on `github.ref`, so a run superseded by a later push is cancelled; `cancel-in-progress` is guarded on the event so a publish is never cancelled part-way through uploading. The three group names (`ci-`, `release-`, `windows-`) must stay distinct or the workflows cancel each other.
- **`config/version` is not in any workflow path filter** — a version-bump-only PR triggers no release build.
- `docker.yml` has no push trigger, so **a Dockerfile change is ungated until a release is cut**.
- `Dockerfile.frontend-artifact` expects a prebuilt `frontend_dist/` in the build context (CI builds the frontend once natively rather than five times under QEMU). It **fails from a bare checkout** — use `Dockerfile` locally.
- `.dockerignore` has no `*.exe` or `.git` entry, while the repo root accumulates ignored multi-hundred-MB `sui*.exe` binaries — they land in the Docker build context.

## Dependency updates

`.github/dependabot.yml` batches minor and patch updates into **one PR per ecosystem** (`gomod`, `github-actions`, `frontend`); majors still arrive on their own. Two behaviours that cost time to rediscover:

- **Editing `dependabot.yml` re-runs dependabot immediately** — it does not wait for the next daily tick. Grouping took effect within minutes of the merge and auto-closed the individual PRs it superseded.
- **A pseudo-version moving to its tagged release never joins a group.** `v0.2.8-0.20250909125414-3aed155119a1` → `v0.2.8` leaves major/minor/patch identical, so dependabot classifies it as neither minor nor patch and it falls outside the group's `update-types`. It gets its own PR, whose `go.sum` hunk sits right next to the group PR's — merge the group first and the leftover **always** conflicts and needs an `@dependabot rebase` comment. A `sing-box` bump usually subsumes it outright, since sing-box's own `go.mod` pulls `sing` forward: doing that bump by hand closed the standalone `sing` PR with no action on it at all.

Two deliberate carve-outs, both easy to "simplify" into a bug:

- `sing-box` is excluded from the gomod group. Bumping it can leave the `core/protocol/` copies stale, and that re-copy is its own piece of work — not something to do inside a PR that also moves five other modules. (It does not always drift: see the protocol-copies note above.)
- Only `typescript` majors are ignored (`typescript-eslint` pins `typescript >=4.8.4 <6.1.0`, still true at 8.66.0; the comment in `dependabot.yml` itself says 8.65.0 and drifts the same way). Nothing else is, because `ignore` suppresses **security** update PRs as well as version ones, and most frontend deps ship inside the SPA.

Every action in `.github/workflows/` is pinned to a commit SHA with a trailing `# vX.Y.Z` comment; dependabot maintains both halves. Don't tidy them back to `@vN` — a tag is a mutable ref, and these workflows build the binaries users run as root.

Grouping does not make CI's `npm ci` any more trustworthy on a frontend PR — re-run the `npm install --package-lock-only` check above before merging one.
