# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

2S-UI is a sing-box web panel — a maintained fork of `alireza0/s-ui`. Module path: `github.com/shenaba/2s-ui`.

Two facts shape almost everything else:

1. **sing-box is embedded as a Go library, not supervised as a subprocess.** `core/` wraps it directly (`core.Box` reimplements sing-box's own `box.Box` so the panel can inject its own trackers). Restarting "the core" is an in-process object teardown, not a process kill.
2. **The frontend compiles into the Go binary.** `frontend/dist/` → `web/html/` → `//go:embed *` in [web/web.go](web/web.go). There is one deployable artifact: `sui`.

## Commands

Go toolchain per `go.mod`: **1.26.4** (`CONTRIBUTING.md` says 1.25 — go.mod is authoritative; CI uses `go-version-file: go.mod`). CGO is required for the SQLite driver.

```bash
./build.sh          # frontend (npm i && npm run build) -> web/html -> go build -o sui
./runSUI.sh         # build.sh, then SUI_DB_FOLDER="db" SUI_DEBUG=true ./sui
```

Panel: http://localhost:2095/app/ (`admin`/`admin`). Sub server: :2096.

### Reproducing the PR gate locally

`ci.yml` is the only thing that runs on a PR, and it is exactly these three commands:

```bash
export BUILD_TAGS=with_quic,with_grpc,with_utls,with_acme,with_gvisor,badlinkname,tfogo_checklinkname0,with_tailscale
go vet -tags "$BUILD_TAGS" ./...
go build -ldflags="-w -s -checklinkname=0" -tags "$BUILD_TAGS" -o /dev/null .
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

`npm run dev` proxies `/app/api` → `localhost:2095`. In DEV only, a custom axios adapter falls back to `src/plugins/mock.ts` when the backend is down (the vite proxy turns `ECONNREFUSED` into a 502, which the adapter reads as "not running") — so **the UI silently renders fake data if you forget to start the Go side**.

### Tests

**There are none.** Zero `*_test.go` in the repo, no frontend unit tests. CI gates compilation and types only. Verification is: build, run, exercise the changed area by hand (both themes, mobile ≤820px, `fa` RTL for UI work). Don't claim a change is tested because CI is green.

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

Adding an endpoint = adding a `case` to the switch **and** a method on `ApiService`. Response convention throughout is `{success, msg, obj}` (`Msg` in `frontend/src/plugins/httputil.ts`).

There is **no per-entity endpoint**: every mutation is `POST api/save` with `{object, action, data}`. On the frontend, that's `Data().save(object, action, data)` in `store/modules/data.ts`.

### The BASE_URL loop

The panel path is a **runtime DB setting**, not a build constant. This spans five files and is easy to break:

1. `web.go` reads `GetWebPath()` and renders the built `index.html` **as a Go template** with `{"BASE_URL": base_url}`.
2. `frontend/index.html` ships `window.BASE_URL = "{{ .BASE_URL }}"`, falling back to `/app/` if the first char is `{` (i.e. unrendered → dev mode). Vite doesn't touch inline script bodies, so the Go template survives the bundle.
3. `vite.config.mts` sets `base: ''` (relative asset URLs), `router` uses `createWebHistory(window.BASE_URL)`, and `plugins/api.ts` sets `axios.defaults.baseURL = "./"`.

**Never hardcode `/app/` and never make axios paths absolute** — it breaks both custom web paths and dev mode.

Related: `vite.config.mts` gives assets **random filenames per build** (not content hashes), because `web.go` serves `assets/` with `Cache-Control: max-age=31536000`.

### Auth

`meta.requiresAuth` in the router is declared but **never read**. Enforcement is server-side on full page load (`web.go`'s `NoRoute` redirects to `login`), plus a reactive client-side catch: any `{success:false, msg:"Invalid login"}` response triggers logout in `httputil.ts`. Session is cookie-based; no token is stored client-side.

### Stats & clients

`core.StatsTracker`/`ConnTracker` are appended to sing-box's router as trackers; a `@every 10s` cron job flushes counters into the `stats` table, bucketed by a unique index on `(resource, tag, date_time, direction)`. Clients hold `Inbounds` as a JSON id array — the "multi-inbound per user" model the project is built around — and editing a client diffs the inbound set and hot-restarts only affected inbounds (`InboundService.RestartInbounds`).

### Version & migrations

`config/version` is `//go:embed`ed — **not** injected via `-X` ldflags. Bumping a release means editing that file in-tree; the git tag and the embedded version can drift. `cmd/migration/` gates on the **2s-ui** version line, not upstream's (users' `dbVersion` tracks 2s-ui releases) — see the comment in `cmd/migration/main.go` before adding one.

### `sui` CLI

`sui -v` · `admin` (`-show/-reset/-username/-password`) · `uri` · `migrate` · `setting` (`-port/-path/-subPort/-subPath`) · `backup` (`-output`, `-exclude=changes,stats`). All except `-v` need a resolvable DB path (`SUI_DB_FOLDER`, else relative to `argv[0]`).

## Frontend conventions

[frontend/DESIGN_SYSTEM.md](frontend/DESIGN_SYSTEM.md) (in Chinese) is authoritative and worth reading before UI work. The load-bearing rules:

- **No UI component library, by design.** ~34 hand-written primitives in `src/components/ui/`. Don't propose adding one (headless included). ECharts is fine — a chart renderer isn't a component library.
- **Tokens only** — `var(--brand)`, `var(--surface-3)`, `var(--text-2)`, `var(--line)`; never hardcode colors. Semantic colors come from `@/plugins/colors.ts`.
- **Theming** = `<html data-theme="dark|light">`. The pre-paint script in `index.html` duplicates the `2sui-theme` localStorage read in `store/modules/app.ts` — change one, change both.
- **No global component registration**; always import explicitly.
- **Never use native `<select>`** — use `<Select>` (it parses slot vnodes so bound values keep their type).
- **Overlays must use `pushOverlay()`** from `components/ui/overlay.ts`; never hand-roll an Esc listener.
- Drawers/modals live in `layouts/drawers/`, not `components/`. Forms split by direction under `components/forms/in|out/`.
- After touching `components/ui/`, `npx vue-tsc --noEmit` must be 0 errors; `scripts/extract-component-api.mjs` regenerates the doc's prop tables.

i18n keys are **structural, not just display**: route `name`s are i18n keys, and toasts are built as `t('actions.'+action) + ' ' + t('objects.'+obj)` — renaming a key silently breaks them. File names ≠ locale keys (`zhcn.ts` → `zhHans`, `zhtw.ts` → `zhHant`). `locales/ui/*.ts` are nominally generated, but the generator needs an external design-handoff dir that isn't in this repo — **treat them as source**.

ESLint is deliberately loosened (`vue/no-mutating-props` is `shallowOnly` because the forms pattern is parent-passes-object/child-mutates-fields; `eqeqeq` off; several rules downgraded to `warn`). **A clean lint run still emits warnings — that's expected.**

## Release quirks

- `release.yml` and `windows.yml` trigger on **both** `push: main` and `release: published`, and have no `concurrency:` block — merging a PR then publishing a release runs the full 7-platform matrix **twice**. Only the `release` run uploads assets (the upload step is gated on `github.event_name == 'release'`).
- **`config/version` is not in any workflow path filter** — a version-bump-only PR triggers no release build.
- `docker.yml` has no push trigger, so **a Dockerfile change is ungated until a release is cut**.
- `Dockerfile.frontend-artifact` expects a prebuilt `frontend_dist/` in the build context (CI builds the frontend once natively rather than five times under QEMU). It **fails from a bare checkout** — use `Dockerfile` locally.
- `.dockerignore` has no `*.exe` or `.git` entry, while the repo root accumulates ignored multi-hundred-MB `sui*.exe` binaries — they land in the Docker build context.
