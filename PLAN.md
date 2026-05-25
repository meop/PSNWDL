# PSNetDL — Plan

A modern reimagining of [PySN](https://github.com/AphelionWasTaken/PySN) and
[PyKG](https://github.com/AphelionWasTaken/PyKG) (the "PySK" you were thinking
of) as a single, library-centric desktop app built in Go + Wails.

PS3-first. Other consoles supported via a mode dropdown, with feature parity
where Sony's endpoints permit.

---

## 1. What PySN + PyKG already do (so we don't lose anything)

### PySN — download manager

- Title Updates from Sony's servers for **PS3 / PS4 / PS Vita**.
- Firmware (`.PUP`) for **PS3 / PS4 / PS5 / Vita** (locale-fanned),
  plus PS4 recovery, Vita fonts, Vita preinst.
- DRM-free Title Updates (PS3 only).
- SHA-1 verification post-download — PS3/Vita hash all but final 32 bytes
  (which is the stored digest), PS4/PS5 hash the whole file.
- Scan RPCS3 `games.yml` and batch-search every title ID found.
- Search-by-serial (`BCUS98114`) or by typing `fw` / `firmware`.
- Settings: download dir, RPCS3 dir (Win only), folder naming
  (`ID - Name` / `Name - ID`), max concurrent downloads (1–100, default 12).
- Per-item Download/Pause/Resume/Cancel + a "Download All (optionally only new)" modal.

### PyKG — extractor / installer

- Decrypts PS3 NPDRM PKGs (retail + debug/homebrew) with AES-CTR,
  derives keystream from `qa_digest` for debug PKGs.
- Parses `PARAM.SFO` → `TITLE_ID`, `APP_VER`, `TITLE`.
- Recursive folder scan for `.pkg`.
- SHA-1 of body vs. trailing 20-byte digest (PackageDigest), skips zero digest
  (homebrew).
- Groups PKGs by `TITLE_ID`, sorts by version, **skips remaining versions of a
  title if any earlier one failed** (correct install ordering).
- Resolves path-traversal PKGs into a virtual PS3 root safely.

### Server endpoints PySN uses (we'll inherit)

| Console | Endpoint                                                                                   |
| ------- | ------------------------------------------------------------------------------------------ |
| PS3     | `https://a0.ww.np.dl.playstation.net/tpl/np/{tid}/{tid}-ver.xml`                           |
| PS4     | `https://gs-sec.ww.np.dl.playstation.net/plo/np/{tid}/{hmac-sha256(tid)}/{tid}-ver.xml`    |
| Vita    | `https://gs-sec.ww.np.dl.playstation.net/pl/np/{tid}/{hmac-sha256(tid)}/{tid}-ver.xml`     |
| FW      | `https://f{locale}01.ps{N}.update.playstation.net/...` (XML for PS4/5/Vita, TXT for PS3)   |
| PS5     | HTTP, fixed obfuscation token `tJMRE80IbXnE9YuG0jzTXgKEjIMoabr6`, firmware-only            |

HMAC keys for PS4 and Vita are embedded in `PySN.py:605-611` and stay valid.

---

## 2. What we're explicitly adding beyond PySN/PyKG

1. **Library is the home screen.** RPCS3 `games.yml` is treated as a first-class
   source, not a checkbox.
2. **Hot-reload `games.yml`.** Filesystem watch; re-resolve when RPCS3 changes
   it. PySN requires a manual re-search.
3. **State reconciliation.** For every title in the library, compute:
   *latest version on server* vs. *highest installed locally* → status badge
   (Up to date / Update available / Missing all updates / Server unreachable).
4. **Auto-sync toggle.** Off by default. When on, any title found to be behind
   is queued automatically; a manual "Sync now" button always works.
5. **One app, two jobs.** Download (PySN) and extract/install to
   `dev_hdd0/game` (PyKG) live in the same UI — extraction can be auto-chained
   after a successful download.
6. **Toml config in `~/.PSNetDL/config.toml`** (XDG-respecting; see §5).
7. **Mode dropdown** for the user-visible "what am I working on?" switch
   (PS3 / PS4 / Vita / PS5 firmware). PS3 is the default.

### Out of scope for v1, flag now

- **DLC sync.** Sony does not expose a public DLC index analogous to
  `*-ver.xml`. PySN never supported DLC. We can either (a) drop DLC from scope
  for v1, or (b) add a per-title manual DLC entry box where the user pastes a
  `content_id` and we attempt `https://a0.ww.np.dl.playstation.net/tpl/np/{tid}/{content_id}/...`
  (works for some DLC, not all). **Recommend: drop from v1, revisit once core
  is shipped.** Flagging because your prompt asked for "updates / dlc".
- PS5 title-update *downloads* (PySN explicitly doesn't support; PS5 firmware
  only).
- PKG → re-encrypt / PUP repacking.

---

## 3. UX problems with PySN/PyKG and how we fix them

| PySN/PyKG annoyance                                                          | PSNetDL fix                                                                                     |
| ---------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| "What does this checkbox do?" (Search Games.yml, Clear List On Search…)     | Library mode is the default landing screen. No magic checkboxes.                                |
| Search box doubles as a magic `fw`/`firmware` keyword                       | Dedicated **Firmware** tab in the sidebar.                                                       |
| "Download All" opens a modal and asks again                                  | Inline `Sync selected` / `Sync all behind` buttons act immediately, no modal.                   |
| `Pause` and `Open` buttons swap to `Resume` and `Cancel` mid-download       | Two stable buttons per row: **state-aware primary** (`Sync / Pause / Resume / Retry`) and **secondary** (`Open folder` always available once anything exists). |
| Per-item progress bar + total progress bar but no per-row throughput        | Each row shows MB/s + ETA; global header shows aggregate.                                       |
| Hash result shown as a tiny coloured label                                  | Status column with icon + tooltip; click to see SHA-1, expected vs. observed.                   |
| Settings hidden behind a Windows-only RPCS3 path field                       | Settings is a left-nav screen, not a modal. RPCS3 path is auto-detected on all platforms first. |
| Two separate apps (PySN + PyKG), drag-and-drop into RPCS3 to install       | Built-in "Install to RPCS3" action per title using PyKG's logic; optional auto-install on download. |
| UI feels unresponsive even though it actually threads                       | Wails frontend runs independently of Go backend; long ops emit progress events, never block.    |
| No way to see history                                                       | Activity log persisted in `~/.PSNetDL/history.jsonl`.                                            |

---

## 4. Architecture (Go + Wails)

```
PSNetDL/
├── cmd/PSNetDL/main.go          # Wails entrypoint
├── wails.json
├── go.mod
├── internal/
│   ├── psn/                     # Sony endpoint clients
│   │   ├── ps3.go               # tpl/np XML + DRM-free branch
│   │   ├── ps4.go               # HMAC, plo/np XML, manifest JSON pieces
│   │   ├── vita.go              # HMAC, pl/np XML
│   │   ├── ps5.go               # firmware only
│   │   ├── firmware.go          # locale fan-out
│   │   └── hash.go              # SHA-1 (PS3/Vita-aware: skip trailing 32 B)
│   ├── pkg/                     # PyKG port
│   │   ├── header.go            # PKG_MAGIC, header parse
│   │   ├── sfo.go               # PARAM.SFO parse
│   │   ├── decrypt.go           # AES-CTR + debug keystream
│   │   ├── extract.go           # streaming extract
│   │   └── verify.go            # PackageDigest verify
│   ├── rpcs3/
│   │   ├── locate.go            # platform-specific games.yml path
│   │   ├── parse.go             # YAML title_id → install dir
│   │   └── watch.go             # fsnotify, debounced
│   ├── library/
│   │   ├── model.go             # Title, Update, InstalledVersion, Status
│   │   ├── reconcile.go         # server-state ∩ local-state → row status
│   │   └── store.go             # cache of last-seen server data
│   ├── sync/
│   │   ├── queue.go             # semaphore-bounded scheduler
│   │   ├── job.go               # download → verify → (optional) extract
│   │   └── events.go            # Wails event emitter for progress
│   ├── config/
│   │   ├── config.go            # toml load/save, defaults, migrations
│   │   └── paths.go             # ~/.PSNetDL, XDG_CONFIG_HOME respect
│   └── app/                     # Wails-bound API surface
│       └── api.go               # methods callable from frontend
└── frontend/                    # Wails frontend (see §6 for stack)
```

### Concurrency model

- One goroutine pool for **search/metadata** (size ~6, like PySN).
- One bounded pool for **downloads** (semaphore, configurable, default 6).
- One pool for **hash verification** (size ~4).
- Extraction runs sequentially per title (PyKG's group-by-`title_id` ordering
  is load-bearing for correctness — earlier versions must extract before later).
- All UI updates are pushed via `runtime.EventsEmit` — frontend never polls.

### Library reconciliation flow

```
games.yml changed ──► parse → [title_id, install_path]
       │
       ▼
For each title (parallel, capped):
   ▶ fetch *-ver.xml from PSN
   ▶ find highest <package version=…>
   ▶ scan local download folder for matching PKGs
   ▶ status = max(local_ver) vs max(server_ver)
   ▶ emit "library:title:updated" event
```

If `autoSync == true`, any title resolving to `UpdateAvailable` or
`MissingAll` is appended to the sync queue immediately.

---

## 5. Config

Path: `~/.PSNetDL/config.toml` (overridable with `$PSNETDL_CONFIG`).
On first run, the directory is created and the file written with sensible
defaults — never silently re-created on subsequent runs.

```toml
schema_version = 1

[paths]
downloads      = "~/PSNetDL/Updates"     # default if user never changes it
rpcs3_games_yml = ""                      # auto-detected; user can override
rpcs3_hdd0_game = ""                      # for "Install to RPCS3"

[library]
auto_sync      = false                    # off by default — user opt-in
folder_format  = "id_name"                # "id_name" | "name_id"
watch_games_yml = true

[network]
max_concurrent_downloads = 6              # PySN default was 12; lower is friendlier
verify_tls               = false          # PSN endpoints are http; matches PySN behavior
request_timeout_seconds  = 15

[ui]
theme = "system"                          # "system" | "light" | "dark"
default_mode = "ps3"                      # ps3 | ps4 | vita | ps5_firmware
```

Defaults match PySN's behavior except `max_concurrent_downloads` (12 → 6,
since Sony rate-limits aggressively) and `auto_sync` (new feature, off).

---

## 6. UI design

### Frontend stack

Recommend **Svelte + TypeScript + Tailwind** (Wails template:
`wails init -t svelte-ts`). Reasons: smallest bundle of the three official
templates, the reactivity model maps cleanly to "server events drive table
rows", and the build output is static (no SSR plumbing to fight Wails).
React + Vite is the obvious alternative if you'd rather stay in React.

### Layout

```
┌──────────────────────────────────────────────────────────────────────┐
│  PSNetDL                  [PS3 ▾]  [● Auto-sync OFF]  [Sync All ▶]  │ ← top bar
├──────────┬───────────────────────────────────────────────────────────┤
│          │  Library — RPCS3                                          │
│ Library  │  ┌─────────────────────────────────────────────────────┐ │
│ Search   │  │ ⬤ Gran Turismo 5      BCUS98114   v1.13 → v1.13    │ │
│ Firmware │  │   ✓ Up to date                       [Open] [Sync] │ │
│ Activity │  ├─────────────────────────────────────────────────────┤ │
│ Settings │  │ ⚠ Demon's Souls       BLUS30443   v1.05 → v1.08    │ │
│          │  │   Missing 3 updates  [▇▇▇▇▇░░░ 62% · 4.1 MB/s]    │ │
│          │  │                            [Pause] [Open] [Cancel] │ │
│          │  ├─────────────────────────────────────────────────────┤ │
│          │  │ ✗ Catherine           BLUS30428   ?    → v1.02     │ │
│          │  │   Server unreachable                  [Retry] [Open]│ │
│          │  └─────────────────────────────────────────────────────┘ │
├──────────┴───────────────────────────────────────────────────────────┤
│  Active: 1 download · 4.1 MB/s · ETA 0:42   [details ▾]              │ ← status bar
└──────────────────────────────────────────────────────────────────────┘
```

Key UX rules:

- **Every clickable element has a hover tooltip** describing what it does.
  No more "what does Search Games.yml mean".
- **Buttons don't shape-shift.** Primary action label changes (`Sync` →
  `Pause`), but the secondary button is always `Open folder` — never repurposed
  as Cancel. Cancel is a separate small icon button that appears only when
  there's something to cancel.
- **Status icons are stable**: ✓ up-to-date, ⚠ behind, ✗ error, ⬤ idle,
  ▶ running, ⏸ paused.
- **Per-row expansion** reveals the individual updates available for that
  title (PySN listed every update as its own row, which scaled badly with a
  full RPCS3 library).
- **Long ops never block the UI** — they emit `sync:progress`, `library:scan`,
  `extract:progress` events.

### Sidebar pages

- **Library** — default. Driven by `games.yml`. Disabled with helpful empty
  state if RPCS3 path isn't set.
- **Search** — manual lookup by title ID (the "arbitrary search" you asked
  for). Same row component as Library.
- **Firmware** — picks console + locale, lists all known PUPs. Replaces
  PySN's `fw` keyword magic.
- **Activity** — append-only log of completed/failed jobs from
  `~/.PSNetDL/history.jsonl`.
- **Settings** — full page, not a modal. Each setting has an inline
  description and a `Restore default` link.

### Mode dropdown

Top-bar dropdown switches the **active console context** for Library/Search/
Firmware. Defaults to PS3. State per mode is preserved when switching back.

---

## 7. Build order

Suggested implementation order, smallest-shippable-thing first:

1. **Project skeleton**: `wails init`, choose Svelte-ts template, wire
   config loader (`~/.PSNetDL/config.toml`), top bar + sidebar shell.
2. **PSN client — PS3 only** (`internal/psn/ps3.go`): fetch `*-ver.xml`,
   parse `<package>` and DRM-free `<url>`, expose `LookupTitle(tid) (Title, error)`.
3. **Manual Search page**: text input → call into `LookupTitle` → render a
   row with one expandable detail per update.
4. **Downloader** (`internal/sync/`): bounded semaphore, streaming
   download with progress events. Hash verify in a separate goroutine.
5. **RPCS3 integration**: locate + parse `games.yml`, render Library page.
6. **Reconciliation**: server-state vs. local-state → status badges.
7. **Auto-sync + `fsnotify` watch** on `games.yml`.
8. **PyKG port** (`internal/pkg/`): start with header read + SFO parse,
   then decryption, then streaming extract. Mirror Python's grouping/ordering
   logic exactly.
9. **"Install to RPCS3" action** per title, optionally auto-chained after
   download (settings toggle).
10. **Firmware page** (PS3 first, then fan out to others).
11. **PS4 + Vita + PS5 firmware** modes: add `psn/ps4.go`, `vita.go`,
    `ps5.go`. The HMAC keys and endpoint shapes are already known.
12. **Activity log**, **Settings page polish**, **packaging** (Wails
    cross-compile for Linux/macOS/Windows).

Each step ships a runnable app — no half-built milestones.

---

## 8. Open questions for you

1. **DLC**: drop from v1 (recommended) or attempt the manual-content-id route?
2. **Auto-install after download**: should this be on by default for users
   who set an `rpcs3_hdd0_game` path, or always opt-in?
3. **Default download root**: `~/PSNetDL/Updates` vs. `~/Games/PS3/Updates`
   vs. XDG `~/.local/share/PSNetDL`?
4. **Frontend stack**: Svelte-ts (recommended above) or do you have a
   preference (React/Vue)?
