# AGENTS.md — notes for AI agents working on PSNWDL

This file orients future coding-agent iterations. Read it before editing. It
captures the project's conventions, the load-bearing invariants (do not break
these), the verification gate, and the recurring failure modes a prior pass
left behind that we then had to clean up.

---

## The one command that defines "done"

```sh
go build ./...
go vet ./...
go test ./...
wails3 generate bindings -clean=true -ts
cd frontend && pnpm run build && pnpm run check
cd .. && wails3 build
```

A change is not finished until **all** of these pass, and `pnpm run check`
reports **`0 errors and 0 warnings`**. The "0 warnings" part is non-negotiable —
warnings accumulate into the visual/behavioral bugs this project was created to
fix. If you can't get to 0/0, say so explicitly rather than declaring success.

## Architecture in one paragraph

Wails v3 desktop app. **Backend = Go** (`app.go` + `internal/*`). **Frontend =
Svelte 5 + TypeScript + Tailwind v4** (`frontend/src/*`). The two halves meet
only through Wails-generated bindings in `frontend/bindings/` — the frontend
never imports Go directly, and Go never imports frontend code. Every public
method is `func (a *App) ...` in `app.go`; events flow Go → frontend via
`application.App.Event.Emit(name, payload)` and are subscribed with
`Events.On(name, ...)`. After **any** change to a Go struct, service method, or
registered event type, run `wails3 generate bindings -clean=true -ts` from the
module root to regenerate the TS models/bindings, then rebuild.

---

## Load-bearing invariants (breaking these is a regression)

1. **No download ever auto-installs.** Installing is always a separate explicit
   user click (`App.InstallLibraryPKGsPS3`) and only considers PKGs in PSNWDL's
   own Library. Do not re-add arbitrary-folder installation, an auto-install
   flag to `jobs.Request`/`Job`, or a post-verify install block in `queue.run`.

2. **Theming uses semantic tokens, never hardcoded colors.** All styling goes
   through `bg-surface`, `text-fg`, `border-border`, `bg-accent`,
   `text-accent-fg`, `bg-success-bg`/`text-success-fg`, etc., defined as CSS
   variables in `style.css` (`:root` = dark, `[data-theme='light']` = light)
   and exposed to Tailwind via `@theme inline`. **Do not** reintroduce
   `slate-900` / `bg-blue-600` / `text-white` etc. — that is exactly what
   caused the light/dark "weird mix."

3. **Accent buttons are theme-specific.** The user rejected identical
   blue-with-white primary buttons in both themes. Keep button colors routed
   through `--c-accent`, `--c-accent-hover`, and `--c-accent-fg`, and tune
   those tokens separately for dark and light mode.

4. **`pkg.parsePKGHeader` / `DiscoverPKGs` must handle real PS3 NPDRM PKGs.**
   The PS4 `PFS0` magic does **not** appear in PS3 PKGs. Discovery reuses the
   `ReadHeader` + `decryptRegion` + `findSFO` path that `Extract` uses, and
   never slurps the whole multi-GB file into memory. If you touch PKG parsing,
   test against an actual PS3 PKG, not a PS4 one.

5. **`activity.Entry.Ts` is a string (RFC3339), not `time.Time`.** Wails' model
   generator has no mapping for `time.Time` and exits non-zero with a noisy
   "Not found: time.Time". Any timestamp that crosses the Go→TS boundary must
   be a pre-formatted `string`.

6. **First paint must already reflect config.** `App.svelte` boot-gates on
   `GetConfig()` before rendering, and `main.ts` applies the theme before
   mount. Do not revert to initializing `mode`/theme inside `onMount` — that
   causes a visible flash + a nav bounce (PS3 → configured mode).

7. **Cross-platform by default.** Anything that shells out (`OpenFolder`,
   future process launches) must branch on `runtime.GOOS` (Windows `explorer`
   / macOS `open` / Linux `xdg-open`). Do not hardcode a Windows-only path.

8. **Subsystems emit to the activity console.** The four advertised scopes
   (`psn`, `jobs`, `library`, `pkg`) all have producers. If you add a new
   subsystem or a long-running operation, emit `activity.Infof/Warnf/Errorf`
   with the right scope so the Activity console narrates it. A scope offered
   as a filter chip with no producer is a dead UI element.

9. **The four workbench panes refresh independently.** Download search and
   Emulator reconciliation are manual-only through their Search and Refresh
   controls. Platform changes refresh only the local Library. Library changes
   may recompute already-cached Download/Emulator row state, but must not parse
   `games.yml` or make PSN requests. Every completed job refreshes Library. Do
   not add file watching, periodic reconciliation, or cross-pane network refreshes.

10. **The queue is application-wide and belongs to Activity.** Download and
    Emulator actions share one backend queue and one concurrency limit. Show
    only active jobs in Activity, remove them from that view at terminal state,
    and keep history in the Activity log. Do not embed a queue table in either
    top pane.

11. **Library storage has explicit content branches.** The default root is
    `~/.psnwdl/library`; files live under `<mode>/firmware/<region>/` or
    `<mode>/title/<TitleID>/`. Keep download destinations, scanning,
    synchronization, Library actions, and documentation on this layout. The app
    is unreleased, so do not add schema migrations or legacy-layout handling.

12. **Download results keep Library matches visible but disabled.** Matching is
    local and reactive against the downloaded-library store: completion makes
    the matching row non-interactive and deletion enables it again when that
    firmware/title result is currently loaded. Do not turn Library changes into
    fresh PSN searches.

13. **Emulator sync is exact.** Per-title Sync removes unexpected files and
    queues every missing server-advertised package. Sync all also removes PS3
    title folders not represented in RPCS3. Never reduce this to a
    highest-version comparison; gaps between versions must remain detectable.

---

## Conventions

### Go
- Idiomatic Go; `gofmt` formatting. Table-driven tests where it makes sense.
- Errors are wrapped with `%w` and returned; never swallowed silently
  (`if err := f(); err == nil { ... }` was a real bug — don't repeat it).
- `internal/` packages should not import `app.go` (no circular deps). The App
  layer orchestrates; packages expose pure functions/structs.

### Frontend (Svelte 5 runes)
- Use **runes** (`$state`, `$derived`, `$effect`, `$props`) — not the legacy
  `let`/reactive-statement Svelte 4 syntax. Stores (`writable`/`derived`) are
  still fine for cross-component shared state (see `app/jobsStore.svelte.ts`).
- When a `$derived` depends on both a store and a rune (e.g. `mode` prop),
  read the store with `$` **inside** the `$derived` body — do not capture the
  rune into a separate store at the top of the script (that triggers
  `state_referenced_locally` and silently breaks reactivity).
- Prefer deriving filtered lists over `{#each}` with empty `{#if}` branches;
  Svelte lint flags the empty branches as warnings.
- Shared UI (`Loading`, `EmptyState`) lives in `components/` and is imported
  by pages — don't inline `<p>Loading…</p>` per page.

### Theming
- To add a new semantic color: add the variable to **both** `:root` and
  `[data-theme='light']` in `style.css`, then map it under `@theme inline` as
  `--color-<name>: var(--c-<name>)`. Components can then use `bg-<name>` etc.

---

## Recurring failure modes to watch for

These all happened in a prior pass and were fixed — they tend to recur:

- **Theme plumbing without the sweep.** Adding CSS variables + a theme store
  but not replacing the hardcoded `slate-*` classes in components. Result:
  dark looks fine, light is a broken mix. Always grep for `slate-|bg-blue-|…`
  after any theme change; expect zero hits.
- **Stubs that aren't wired.** A type/interface/store/component exists but no
  caller uses it (dead `aggregateStatus`, never-imported `<Loading>`,
  never-emitted activity scopes). Existence ≠ done. Grep for call sites.
- **Backend change without `wails3 generate bindings -clean=true -ts`.** The TS bindings go stale
  and the frontend compiles against ghosts. Always regenerate + rebuild.
- **`go test` passing declared as success.** Tests cover unit logic, not the
  wiring. A fully-built, type-checked, 0-warning frontend + binding generation
  exit 0 is the real gate.
- **Platform assumptions.** Hardcoded `explorer`, hardcoded `~` expansion that
  only works in one shell, etc. Default to cross-platform.

---

## Where things live (quick lookup)

| You want to… | Look at |
|---|---|
| Add a backend method the UI can call | `app.go` (add `func (a *App)…`), then regenerate Wails 3 bindings |
| Add a download-related feature | `internal/jobs/queue.go` + `types.go` |
| Change a PSN endpoint / add a console | `internal/psn/` (`ps3.go`, `ps4.go`, `vita.go`, `firmware.go`) |
| Touch PKG parsing/extraction | `internal/pkg/` (`header.go`, `decrypt.go`, `extract.go`, `sfo.go`) |
| Change library reconciliation logic | `internal/library/reconcile.go` |
| Add a page | `frontend/src/pages/`, register in `frontend/src/app/types.ts` and `App.svelte` visibility rules |
| Add a shared store | `frontend/src/app/*.svelte.ts` |
| Change colors / add a theme token | `frontend/src/style.css` |

---

## Current TODO State

`meop/PSNWDL/TODO` currently has no open items. `PLAN.md` was the original build
plan and has been removed because it was stale. If a new TODO appears, keep
README/AGENTS aligned with product decisions when implementing it.
