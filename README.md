# PSNWDL

A cross-platform desktop app for browsing and caching **official** game-update
and firmware packages from the PlayStation Network update endpoints — for PS3,
PS4, PSVita, and PS5. Downloads are cached to disk for offline re-installation
into emulators (RPCS3 in particular) or firmware archiving.

PSNWDL does **not** patch, decrypt, or bypass any DRM. It only fetches the same
public, signed update packages that the consoles themselves request, and caches
them verbatim. PS3 PKG *extraction* (decryption of the NPDRM wrapper to files)
is performed locally by the app so updates can be dropped straight into an
RPCS3 `dev_hdd0/game` tree — but the PKGs themselves are unmodified.

> **Legal note:** Use only with titles and firmware you legitimately own.
> Sony's update endpoints are public; redistribution of downloaded packages is
> not. This tool is for personal archival / emulator setup.

---

## Features

- **Title search** — look up a Title ID (e.g. `BCUS98114`) on PS3/PS4/Vita and
  see every published update version with size, hash, and download URL. PS3 can
  optionally include DRM-free package rows when Sony advertises them.
- **Latest firmware browser** — per-console firmware currently advertised by
  Sony's regional update lists, deduplicated by version.
- **Shared download queue** — one application-wide concurrency limit covers
  Download and Emulator actions. Active jobs and cancellation live in Activity;
  packages are verified with final size/SHA-1 checks and automatic retries.
- **Library manager** — shows downloaded title updates and firmware files,
  checks for newer versions of titles/firmware you already have, and supports
  checkbox deletion by title folder or individual file.
- **RPCS3 library reconciliation (PS3 only)** — reads your RPCS3 `games.yml` +
  `dev_hdd0/game` to show, per installed title: installed version → latest
  server version, with status badges (up to date / update available / missing /
  unreachable). One-click per-title update downloads.
- **PyKG-style batch install (PS3)** — point the app at a folder of local PS3
  `.pkg` files you are entitled to use; it discovers them recursively, groups
  by title, orders by version, and streams the extracts into `dev_hdd0/game`,
  stopping a title's group on first failure.
- **Activity** — live active-job controls plus a log of every fetch, reconcile,
  download, verify, and extract, filterable by scope (`psn` / `jobs` /
  `library` / `pkg`).
- **Light / dark / system theme** — applies live; persists in config.

---

## Inspiration

PSNWDL is inspired by AphelionWasTaken's
[PySN](https://github.com/AphelionWasTaken/PySN) and
[PyKG](https://github.com/AphelionWasTaken/PyKG), but it is not a 1:1 clone.
It combines PySN-style title/firmware discovery with PyKG-style PS3 package
inspection/extraction, then adds its own persistent library, workbench UI,
activity log, and RPCS3 reconcile/download/install flow.

| Capability | PySN | PyKG | PSNWDL |
|------------|------|------|--------|
| PS3/PS4/Vita title update lookup | Yes | No | Yes |
| PS5 title update lookup | Not supported | No | Not supported |
| PS3 DRM-free update rows | Yes | No | Yes, optional |
| Latest firmware by region | Yes | No | Yes |
| PS4 manifest-piece downloads | Yes | No | Yes |
| Download queue + verification | Yes | No | Yes, with size checks and automatic retries |
| Configurable concurrent downloads | Yes | No | Yes |
| Fixed cache/library view | No | No | Yes |
| RPCS3 installed-title reconcile | Partial | No | Yes |
| Queue missing RPCS3 updates | Partial | No | Yes |
| Recursive PS3 PKG folder install | No | Yes | Yes |
| PARAM.SFO metadata parsing | No | Yes | Yes |
| Retail/debug PS3 NPDRM extraction | No | Yes | Yes |
| Folder naming format options | Yes | No | No; fixed cache layout by design |

---

## Requirements

| Tool | Version | Why |
|------|---------|-----|
| [Go](https://go.dev/dl/) | ≥ 1.25 | backend and the pinned Wails 3 toolchain |
| [Node.js](https://nodejs.org/) | ≥ 20 | frontend toolchain |
| [pnpm](https://pnpm.io/) | ≥ 9 | frontend package manager |
| [Wails CLI](https://v3.wails.io) | v3 | build / dev harness |

Install the Wails CLI once:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@latest
```

### Runtime prerequisites

- **For Emulator install/extraction**, you need [RPCS3](https://rpcs3.net/) and its
  `games.yml` plus `dev_hdd0/game` paths (set in Settings).
- **No prerequisites for PS4/Vita/PS5** — those only fetch and cache the
  signed packages; they are not extracted.
- **For local PS3 PKG folder install**, PSNWDL follows PyKG's model: it scans a
  folder you choose. The app does not create or dump those package files.

---

## Build & run

### Live development (hot reload)

```sh
wails3 dev
```

Runs the Vite dev server with hot reload and launches the native application.
The development server listens on `http://127.0.0.1:9245`; the Wails window
connects to it automatically.

### Production build

```sh
wails3 build
```

Outputs the platform-native binary into `bin/`. Run `wails3 task package` to
create the host platform's installer/package. The generated Wails 3 Taskfiles
also provide Docker-backed cross-compilation tasks where the target requires
platform tooling.

---

## Project layout

```
PSNWDL/
├── app.go                 # Wails-bound methods (the app's public API surface)
├── main.go                # Wails app bootstrap
├── Taskfile.yml           # Wails 3 build/dev/package entry points
├── build/
│   ├── config.yml         # product metadata + dev-mode configuration
│   └── {windows,darwin,linux}/ # platform Taskfiles and packaging assets
├── internal/
│   ├── activity/          # in-memory log ring buffer + event emitter
│   ├── config/            # config.toml load/save, defaults, path helpers
│   ├── jobs/              # download queue: enqueue/pause/resume/retry/verify
│   ├── library/           # games.yml reconciliation, PARAM.SFO version compare
│   ├── pkg/               # PS3 NPDRM PKG header parse, decrypt, extract
│   ├── psn/               # PS3/PS4/PS5/Vita update-XML + firmware fetchers
│   └── rpcs3/             # games.yml parsing, installed-title discovery
└── frontend/
    ├── src/
    │   ├── app/           # stores, theme, types, startup sizing
    │   ├── components/    # shared Loading component
    │   ├── pages/         # Workbench, Activity log, Settings overlay
    │   ├── App.svelte     # shell: mode selector, settings/about overlays
    │   ├── main.ts        # mount + pre-paint config load (no theme flash)
    │   └── style.css      # semantic color tokens (@theme inline) for theming
    └── bindings/          # generated Wails 3 TS bindings (do not hand-edit)
```

---

## Configuration

Config lives at `~/.psnwdl/config.toml` (created with defaults on first run):

```toml
schema_version = 2

[rpcs3]
games_yml = ""     # absolute path to RPCS3/games.yml (auto-detectable)
hdd0_game = ""     # absolute path to RPCS3/dev_hdd0/game (for PS3 install)

[storage]
library_dir = "/home/you/.psnwdl/library"  # base folder for downloaded files

[network]
max_concurrent_downloads = 5
verify_tls = false
request_timeout_seconds = 15 # metadata requests and download connection/header timeout
retry_count = 3

[ui]
theme = "system"            # system | dark | light
default_mode = "ps3"        # ps3 | ps4 | psvita | ps5
default_download = "firmware" # firmware | title
```

`home_dir` is exposed to the UI (resolved at runtime) but never persisted.
`verify_tls` is kept in the config file for advanced diagnostics; the Settings
UI leaves it off because several Sony endpoints use certificates that fail
normal desktop validation.

### Cache layout

Downloaded files are separated by platform and content type. Firmware is grouped
by locale under `firmware`; title updates are grouped by title ID under `title`:

```
~/.psnwdl/library/
├── ps3/
│   ├── firmware/us/firmware_4.93.pup
│   └── title/BCUS98114/BCUS98114_01.05.pkg
├── ps4/
│   ├── firmware/<locale>/<firmware>.pup
│   └── title/CUSA00000/<update>.pkg
├── ps5/firmware/us/firmware_26.04-13.40.00.pup
└── …
```

On the first schema-v3 launch, the former `<mode>/updates/<TitleID>` layout is
migrated in place, and flat firmware files are placed under `firmware/unknown`
because their locale cannot be recovered from the old path. The former default
root `~/.psnwdl/download` is moved to `~/.psnwdl/library`; configured custom
roots remain custom.

Downloads are stored as the raw signed packages Sony ships. PS3 extraction into
RPCS3 is an explicit Emulator action.

---

## How the API surface is wired

The frontend calls Go exclusively through Wails-generated bindings in
`frontend/bindings/PSNWDL/`. `main.go` registers `App` as a Wails service, and
every exported `func (a *App)` in `app.go` becomes a typed TS function. If you
change a Go signature or add/remove a method:

```sh
wails3 generate bindings -clean=true -ts
```

This must exit `0`. If it prints `Not found: time.Time`, a struct field is
using `time.Time` (or another unmapped type) — use an RFC3339 `string` instead
(see `activity.Entry.Ts`).

---

## Verifying a clean build

```sh
go build ./...
go vet ./...
go test ./...
wails3 generate bindings -clean=true -ts
cd frontend && pnpm install && pnpm run build && pnpm run check
cd .. && wails3 build
```

`pnpm run check` should report **0 errors and 0 warnings**. If it doesn't, fix
before considering the work done.

---

## License

Personal-use archival tool. No warranty. Do not redistribute downloaded
packages — they are Sony's signed content.
