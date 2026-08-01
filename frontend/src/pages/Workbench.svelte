<script lang="ts">
  import { onMount } from 'svelte'
  import type { Mode } from '../app/types'
  import {
    AutoDetectGamesYML,
    DeleteLibraryItems,
    EnqueueDownload,
    InstallFolderPS3,
    InstallJob,
    ListDownloadLibrary,
    OpenFolder,
    PickDirectory,
    ReconcileLibraryPS3,
    SearchPS3,
    SearchPS4,
    SearchVita,
    SyncAllPS3,
    SyncTitlePS3,
  } from '../../bindings/PSNWDL/app'
  import { Events } from '@wailsio/runtime'
  import * as jobs from '../../bindings/PSNWDL/internal/jobs'
  import * as psn from '../../bindings/PSNWDL/internal/psn'
  import type * as config from '../../bindings/PSNWDL/internal/config'
  import type * as downloads from '../../bindings/PSNWDL/internal/downloads'
  import type * as library from '../../bindings/PSNWDL/internal/library'
  import { cache, ensureFirmware, fetching } from '../app/firmwareStore.svelte'
  import { jobsList } from '../app/jobsStore.svelte'
  import { libraryState as emulatorState } from '../app/libraryStore.svelte'
  import { downloadLibraryState as libraryState } from '../app/downloadLibraryStore.svelte'
  import { queuedKey, searchState } from '../app/searchStore.svelte'
  import Activity from './Activity.svelte'

  interface Props {
    mode: Mode
    defaultDownload?: Source
    appConfig: config.Config
  }

  type Source = 'title' | 'firmware'
  type Title = downloads.Title
  type File = downloads.File
  const ACTIVE_JOB_STATES = new Set(['queued', 'downloading', 'paused', 'resuming', 'verifying'])
  const EMULATOR_COLUMN_LABELS = ['Title', 'Status', 'Action']

  let { mode, defaultDownload = 'firmware', appConfig }: Props = $props()

  let source = $state<Source>('firmware')
  let downloadError = $state<string | null>(null)
  let emulatorError = $state<string | null>(null)
  let syncingAll = $state(false)
  let emulatorSyncJobIDs = $state<string[]>([])
  let emulatorInstallJobIDs = $state<string[]>([])
  let installingDone = $state(false)
  let installingFolder = $state(false)
  let includeDRMFree = $state(false)
  let lastMode = $state<Mode>('ps3')
  let emulatorConfigKey = $state('')
  let expandedLibraryTitles = $state<Record<string, boolean>>({})
  let downloadColumnWidths = $state([100, 90, 180, 80, 120])
  let emulatorColumnWidths = $state([260, 150, 120])
  let downloadTableMinWidth = $state(0)
  let emulatorTableMinWidth = $state(0)
  let titleState = $derived(searchState[mode])
  let normalizedID = $derived(titleState.titleID.trim().toUpperCase())
  let canSearch = $derived(/^[A-Z]{4}\d{5}$/.test(normalizedID) && !titleState.loading && mode !== 'ps5')
  let canSourceSearch = $derived(source === 'firmware' ? $fetching !== mode : canSearch)
  let cachedFirmware = $derived($cache[mode] ?? null)
  let downloadColumnLabels = $derived(['Kind', 'Version', source === 'firmware' ? 'Locale' : 'Scope', 'Size', 'Action'])
  let firmwareLoading = $derived($fetching === mode && !cachedFirmware?.list)
  let selectedLibraryCount = $derived(libraryState.selected.length)
  let finishedPS3Jobs = $derived(
    $jobsList.filter(
      (job) => job.mode === 'ps3' && (job.kind || 'title_update') !== 'firmware' && job.state === 'done' && !job.installed_to
    )
  )

  $effect(() => {
    if (mode !== lastMode) {
      source = normalizedSource(defaultDownload, mode)
      if (mode !== 'ps3') includeDRMFree = false
      libraryState.selected = []
      lastMode = mode
      if (mode === 'ps3' && emulatorState.initialized) void refreshEmulator()
    }
    downloadError = null
  })

  $effect(() => {
    source = normalizedSource(defaultDownload, mode)
  })

  onMount(async () => {
    if (!libraryState.initialized) {
      libraryState.initialized = true
      await refreshLibrary()
    }
    await ensureEmulatorBooted()
  })

  $effect(() => {
    if (!emulatorState.initialized) return
    const nextKey = emulatorConfigSignature(appConfig)
    hydrateEmulatorConfig(appConfig)
    if (nextKey === emulatorConfigKey) return
    emulatorConfigKey = nextKey
    if (mode === 'ps3') void refreshEmulator()
  })

  $effect(() => {
    const trackedIDs = emulatorInstallJobIDs
    if (trackedIDs.length === 0) return

    const trackedJobs = trackedIDs.map((id) => $jobsList.find((job) => job.id === id))
    if (trackedJobs.some((job) => !job)) return
    const finished = trackedJobs.every(
      (job) => job?.state === 'failed' || job?.state === 'canceled' || (job?.state === 'done' && !!job.installed_to)
    )
    if (!finished) return

    emulatorInstallJobIDs = []
    installingDone = false
    if (mode === 'ps3') void refreshEmulator()
  })

  $effect(() => {
    const offs = [
      Events.On('downloads:updated', ({ data: next }) => {
        libraryState.titles = Array.isArray(next) ? next : []
        libraryState.loading = false
        pruneLibrarySelection()
      }),
      Events.On('downloads:error', ({ data: msg }) => {
        libraryState.error = msg
        libraryState.loading = false
      }),
    ]
    return () => offs.forEach((off) => off())
  })

  $effect(() => {
    const trackedIDs = emulatorSyncJobIDs
    if (trackedIDs.length === 0) return

    const trackedJobs = trackedIDs.map((id) => $jobsList.find((job) => job.id === id)).filter(Boolean)
    if (trackedJobs.length !== trackedIDs.length || trackedJobs.some((job) => ACTIVE_JOB_STATES.has(String(job?.state)))) {
      return
    }

    emulatorSyncJobIDs = []
    if (mode === 'ps3') void refreshEmulator()
  })

  let allDownloadRows = $derived.by(() => {
    const rows: DownloadRow[] = []
    if (source === 'title' && titleState.result) {
      for (const update of titleState.result.updates ?? []) {
        rows.push({
          key: queuedKey(update.version, update.url),
          kind: update.drm_type === 'drm_free' ? 'DRM-free' : 'Title',
          titleId: titleState.result.id,
          titleName: titleState.result.name || titleState.result.id,
          version: update.version,
          size: update.size,
          url: update.url,
          sha1sum: update.sha1sum,
          systemVersion: update.system_version,
          update,
        })
      }
    }
    if (source === 'firmware') {
      for (const entry of cachedFirmware?.list?.entries ?? []) {
        const update = new psn.Update({
          version: entry.version,
          size: entry.size || 0,
          sha1sum: entry.sha1sum || '',
          url: entry.url,
        })
        rows.push({
          key: firmwareKey(entry),
          kind: 'Firmware',
          titleId: 'firmware',
          titleName: `${mode.toUpperCase()} ${entry.type || 'Firmware'} ${entry.version}`,
          version: entry.version,
          size: entry.size || 0,
          url: entry.url,
          sha1sum: entry.sha1sum || '',
          locale: entry.locale,
          type: entry.type || 'Firmware',
          update,
        })
      }
    }
    return rows.sort((a, b) => compareVersion(b.version, a.version))
  })
  let downloadRows = $derived(allDownloadRows)

  interface DownloadRow {
    key: string
    kind: 'Title' | 'DRM-free' | 'Firmware'
    titleId: string
    titleName: string
    version: string
    size: number
    url: string
    sha1sum: string
    locale?: string
    type?: string
    systemVersion?: string
    update: psn.Update
  }

  let modeLibraryTitles = $derived(libraryState.titles.filter((title) => title.mode === mode))
  let firmwareLibraryTitles = $derived(modeLibraryTitles.filter((title) => title.title_id === 'firmware'))
  let gameLibraryTitles = $derived(modeLibraryTitles.filter((title) => title.title_id !== 'firmware'))

  async function ensureEmulatorBooted() {
    if (emulatorState.initialized) return
    hydrateEmulatorConfig(appConfig)
    emulatorConfigKey = emulatorConfigSignature(appConfig)
    emulatorState.detectedPath = await AutoDetectGamesYML()
    emulatorState.initialized = true
    if (mode === 'ps3') await refreshEmulator()
  }

  function hydrateEmulatorConfig(next: config.Config) {
    emulatorState.cfg = next
    emulatorState.gamesYMLInput = next.rpcs3.games_yml
    emulatorState.hdd0Input = next.rpcs3.hdd0_game
  }

  function emulatorConfigSignature(next: config.Config): string {
    return `${next.rpcs3.games_yml}\0${next.storage.library_dir}`
  }

  async function searchTitle() {
    if (!canSearch) return
      titleState.loading = true
      titleState.error = null
      titleState.result = null
      downloadError = null
    try {
      switch (mode) {
        case 'ps3':
          titleState.result = await SearchPS3(normalizedID, includeDRMFree)
          break
        case 'ps4':
          titleState.result = await SearchPS4(normalizedID)
          break
        case 'psvita':
          titleState.result = await SearchVita(normalizedID)
          break
        case 'ps5':
          titleState.error = 'PS5 has no public title-update index'
          break
      }
    } catch (e) {
      titleState.error = e instanceof Error ? e.message : String(e)
    } finally {
      titleState.loading = false
    }
  }

  async function refreshFirmware(force = false) {
    downloadError = null
    try {
      await ensureFirmware(mode, force)
    } catch (e) {
      downloadError = e instanceof Error ? e.message : String(e)
    }
  }

  async function refreshSource() {
    if (source === 'firmware') {
      await refreshFirmware(true)
    } else {
      await searchTitle()
    }
  }

  async function enqueueRow(row: DownloadRow) {
    const req = new jobs.Request({
      title_id: row.titleId,
      title_name: row.titleName,
      mode,
      locale: row.locale || '',
      kind:
        row.kind === 'Firmware'
          ? 'firmware'
          : row.kind === 'DRM-free'
            ? 'title_update_drm_free'
            : undefined,
      update: row.update,
    })
    await EnqueueDownload(req)
  }

  async function enqueueSingle(row: DownloadRow) {
    if (isQueued(row) || downloaded(row)) return
    downloadError = null
    try {
      await enqueueRow(row)
    } catch (e) {
      downloadError = e instanceof Error ? e.message : String(e)
    }
  }

  async function refreshLibrary() {
    libraryState.loading = true
    libraryState.error = null
    try {
      libraryState.titles = (await ListDownloadLibrary()) ?? []
      pruneLibrarySelection()
    } catch (e) {
      libraryState.error = e instanceof Error ? e.message : String(e)
      libraryState.titles = []
    } finally {
      libraryState.loading = false
    }
  }

  async function deleteSelectedLibrary() {
    if (libraryState.selected.length === 0) return
    libraryState.deleting = true
    libraryState.error = null
    try {
      await DeleteLibraryItems(libraryState.selected)
      libraryState.selected = []
      await refreshLibrary()
    } catch (e) {
      libraryState.error = e instanceof Error ? e.message : String(e)
    } finally {
      libraryState.deleting = false
    }
  }

  async function refreshEmulator() {
    if (mode !== 'ps3') return
    if (isMissingGamesConfig()) {
      emulatorState.rows = []
      emulatorState.loadError = null
      return
    }
    emulatorState.loading = true
    emulatorState.loadError = null
    emulatorError = null
    try {
      emulatorState.rows = (await ReconcileLibraryPS3()) ?? []
    } catch (e) {
      emulatorState.rows = []
      emulatorState.loadError = e instanceof Error ? e.message : String(e)
    } finally {
      emulatorState.loading = false
    }
  }

  async function syncTitle(titleID: string) {
    emulatorError = null
    try {
      const jobIDs = (await SyncTitlePS3(titleID)) ?? []
      trackEmulatorJobs(jobIDs)
      if (jobIDs.length === 0) await refreshEmulator()
    } catch (e) {
      emulatorError = e instanceof Error ? e.message : String(e)
    }
  }

  async function syncAllNeeded() {
    syncingAll = true
    emulatorError = null
    try {
      const jobIDs = (await SyncAllPS3()) ?? []
      trackEmulatorJobs(jobIDs)
      if (jobIDs.length === 0) await refreshEmulator()
    } catch (e) {
      emulatorError = e instanceof Error ? e.message : String(e)
    } finally {
      syncingAll = false
    }
  }

  async function installFinishedPS3Jobs() {
    if (finishedPS3Jobs.length === 0) return
    installingDone = true
    emulatorInstallJobIDs = finishedPS3Jobs.map((job) => job.id)
    emulatorError = null
    try {
      for (const job of finishedPS3Jobs) {
        await InstallJob(job.id)
      }
    } catch (e) {
      emulatorInstallJobIDs = []
      installingDone = false
      emulatorError = e instanceof Error ? e.message : String(e)
    }
  }

  async function installPKGFolder() {
    if (!emulatorState.cfg || isMissingInstallConfig()) return
    const picked = await PickDirectory('Select PS3 PKG folder', emulatorState.cfg.storage.library_dir)
    if (!picked) return
    installingFolder = true
    emulatorError = null
    try {
      await InstallFolderPS3(picked, emulatorState.hdd0Input)
      await refreshEmulator()
    } catch (e) {
      emulatorError = e instanceof Error ? e.message : String(e)
    } finally {
      installingFolder = false
    }
  }

  function pruneLibrarySelection() {
    const valid = new Set<string>()
    for (const title of libraryState.titles ?? []) {
      for (const file of title.files ?? []) valid.add(file.path)
    }
    libraryState.selected = libraryState.selected.filter((path) => valid.has(path))
  }

  function isQueued(row: DownloadRow): boolean {
    return $jobsList.some(
      (job) =>
        ACTIVE_JOB_STATES.has(String(job.state)) &&
        job.mode === mode &&
        job.update?.url === row.url
    )
  }

  function downloaded(row: DownloadRow): boolean {
    const cachedTitle = libraryState.titles.find(
      (title) =>
        title.mode === mode &&
        title.title_id === row.titleId &&
        (row.kind !== 'Firmware' || title.locale === row.locale)
    )
    if (!cachedTitle) return false
    const version = row.kind === 'DRM-free' ? `${row.version}_drm_free` : row.version
    return (cachedTitle.files ?? []).some((file) => file.version === version)
  }

  function isMissingGamesConfig(): boolean {
    return !emulatorState.gamesYMLInput
  }

  function isMissingInstallConfig(): boolean {
    return !emulatorState.hdd0Input
  }

  function selected(path: string): boolean {
    return libraryState.selected.includes(path)
  }

  function setSelected(path: string, checked: boolean) {
    if (checked) {
      if (!libraryState.selected.includes(path)) libraryState.selected = [...libraryState.selected, path]
      return
    }
    libraryState.selected = libraryState.selected.filter((p) => p !== path)
  }

  function toggleTitle(title: Title, checked: boolean) {
    const childPaths = new Set((title.files ?? []).map((file) => file.path))
    const next = new Set(libraryState.selected)
    for (const path of childPaths) {
      if (checked) next.add(path)
      else next.delete(path)
    }
    libraryState.selected = [...next]
  }

  function filesForTitles(titles: Title[]): File[] {
    return titles.flatMap((title) => title.files ?? [])
  }

  function totalFileSize(files: File[]): number {
    return files.reduce((total, file) => total + file.size, 0)
  }

  function fileSelectionState(files: File[]): 'none' | 'some' | 'all' {
    if (files.length === 0) return 'none'
    const count = files.filter((file) => selected(file.path)).length
    if (count === 0) return 'none'
    return count === files.length ? 'all' : 'some'
  }

  function toggleFiles(files: File[], checked: boolean) {
    const next = new Set(libraryState.selected)
    for (const file of files) {
      if (checked) next.add(file.path)
      else next.delete(file.path)
    }
    libraryState.selected = [...next]
  }

  function titleSelectionState(title: Title): 'none' | 'some' | 'all' {
    const files = title.files ?? []
    if (files.length === 0) return 'none'
    const count = files.filter((file) => selected(file.path)).length
    if (count === 0) return 'none'
    return count === files.length ? 'all' : 'some'
  }

  function titleExpanded(path: string): boolean {
    return expandedLibraryTitles[path] ?? true
  }

  function toggleTitleExpanded(path: string) {
    expandedLibraryTitles[path] = !titleExpanded(path)
  }

  function trackEmulatorJobs(jobIDs: string[]) {
    emulatorSyncJobIDs = [...new Set([...emulatorSyncJobIDs, ...jobIDs])]
  }

  function titleDownloadInProgress(titleID: string): boolean {
    return $jobsList.some(
      (job) => emulatorSyncJobIDs.includes(job.id) && job.title_id === titleID && ACTIVE_JOB_STATES.has(String(job.state))
    )
  }

  function measureColumns(event: Event, table: 'download' | 'emulator'): { widths: number[]; total: number } {
    const tableElement = (event.currentTarget as HTMLElement).closest('table')
    const cells = tableElement ? Array.from(tableElement.querySelectorAll<HTMLTableCellElement>('thead th')) : []
    const measured = cells.map((cell) => cell.getBoundingClientRect().width)
    const widths = table === 'download' ? downloadColumnWidths : emulatorColumnWidths
    if (measured.length === widths.length) {
      widths.splice(0, widths.length, ...measured)
    }
    const total = tableElement?.getBoundingClientRect().width ?? widths.reduce((sum, width) => sum + width, 0)
    if (table === 'download') downloadTableMinWidth = total
    else emulatorTableMinWidth = total
    return { widths, total }
  }

  function setTableMinWidth(table: 'download' | 'emulator', width: number) {
    if (table === 'download') downloadTableMinWidth = width
    else emulatorTableMinWidth = width
  }

  function resizeColumn(event: PointerEvent, table: 'download' | 'emulator', index: number) {
    event.preventDefault()
    const measured = measureColumns(event, table)
    const widths = measured.widths
    const startX = event.clientX
    const starts = [...widths]

    const move = (nextEvent: PointerEvent) => {
      const delta = Math.max(64 - starts[index], nextEvent.clientX - startX)
      widths[index] = starts[index] + delta
      setTableMinWidth(table, measured.total + delta)
    }
    const stop = () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', stop)
    }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', stop)
  }

  function resizeColumnWithKeyboard(event: KeyboardEvent, table: 'download' | 'emulator', index: number) {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
    event.preventDefault()
    const measured = measureColumns(event, table)
    const delta = event.key === 'ArrowLeft' ? -8 : 8
    const nextWidth = Math.max(64, measured.widths[index] + delta)
    const applied = nextWidth - measured.widths[index]
    measured.widths[index] = nextWidth
    setTableMinWidth(table, measured.total + applied)
  }

  function formatSize(bytes: number | undefined): string {
    if (!bytes || bytes <= 0) return '-'
    if (bytes < 1024) return `${bytes} B`
    const units = ['KB', 'MB', 'GB', 'TB']
    let v = bytes / 1024
    let i = 0
    while (v >= 1024 && i < units.length - 1) {
      v /= 1024
      i++
    }
    return `${v.toFixed(1)} ${units[i]}`
  }

  function compareVersion(a: string, b: string): number {
    return a.localeCompare(b, undefined, { numeric: true, sensitivity: 'base' })
  }

  function normalizedSource(value: Source, currentMode: Mode): Source {
    return currentMode === 'ps5' || value !== 'title' ? 'firmware' : 'title'
  }

  function firmwareKey(entry: psn.FirmwareEntry): string {
    return `${entry.locale}-${entry.type || 'Firmware'}-${entry.version}-${entry.url}`
  }

  function labelForFile(file: File): string {
    const version = file.version ? `v${file.version}` : 'unknown version'
    return `${version} · ${formatSize(file.size)}`
  }

  const STATUS_BADGE: Record<string, string> = {
    all_downloaded: 'bg-success-bg text-success-fg',
    some_downloaded: 'bg-warn-bg text-warn-fg',
    none_downloaded: 'bg-error-bg text-error-fg',
    no_updates: 'bg-surface-2 text-muted',
    unreachable: 'bg-error-bg text-error-fg',
  }

  const STATUS_LABEL: Record<string, string> = {
    all_downloaded: 'All downloaded',
    some_downloaded: 'Some downloaded',
    none_downloaded: 'None downloaded',
    no_updates: 'No updates',
    unreachable: 'Server unreachable',
  }
</script>

<div class="workbench">
  <section class="panel flex min-h-0 flex-col overflow-hidden">
    <div class="panel-head">
      <div>
        <h2>Download</h2>
        <p>{mode.toUpperCase()} discovery and queueing</p>
      </div>
      <form
        class="flex min-w-0 flex-wrap items-center justify-end gap-2"
        onsubmit={(e) => {
          e.preventDefault()
          refreshSource()
        }}
      >
        <label
          class="flex w-28 items-center gap-1 text-xs text-muted"
          class:invisible={source !== 'title' || mode !== 'ps3'}
          title="Include alternate DRM-free package URLs published in PS3 update metadata"
        >
          <input type="checkbox" bind:checked={includeDRMFree} disabled={source !== 'title' || mode !== 'ps3'} />
          Include DRM-free
        </label>
        <input
          bind:value={titleState.titleID}
          placeholder="BCUS98114"
          aria-label="Title ID"
          maxlength="9"
          disabled={source !== 'title'}
          class="input header-control h-8 w-32 px-2"
          class:invisible={source !== 'title'}
        />
        <select bind:value={source} aria-label="Download type" class="input header-control h-8 px-2">
          <option value="firmware">Firmware</option>
          {#if mode !== 'ps5'}<option value="title">Title</option>{/if}
        </select>
        <button type="submit" disabled={!canSourceSearch} class="btn btn-secondary">Search</button>
      </form>
    </div>

    {#if titleState.error || downloadError}
      <div class="border-b border-error/40 bg-error-bg px-3 py-2 text-xs text-error-fg">
        {titleState.error || downloadError}
      </div>
    {/if}

    <div class="min-h-0 flex-1 overflow-auto">
      {#if firmwareLoading}
        <div class="empty">Loading latest firmware</div>
      {:else if downloadRows.length === 0}
        <div class="empty">
          {source === 'title' ? 'Title update results' : 'Latest firmware by region'}
        </div>
      {:else}
        <table class="data-table table-fixed" style={downloadTableMinWidth ? `min-width: ${downloadTableMinWidth}px` : undefined}>
          <colgroup>
            {#each downloadColumnWidths as width, index (index)}
              <col style={`width: ${width}px`} />
            {/each}
          </colgroup>
          <thead>
            <tr>
              {#each downloadColumnLabels as label, index (label)}
                <th>
                  {label}
                  <button
                    type="button"
                    class="column-resizer"
                    aria-label={`Resize ${label} column`}
                    onpointerdown={(event) => resizeColumn(event, 'download', index)}
                    onkeydown={(event) => resizeColumnWithKeyboard(event, 'download', index)}
                  ></button>
                </th>
              {/each}
            </tr>
          </thead>
          <tbody>
            {#each downloadRows as row (row.key)}
              <tr aria-disabled={downloaded(row) || isQueued(row)}>
                <td>{row.kind}</td>
                <td class="font-mono">v{row.version}</td>
                <td class="truncate text-muted">
                  {#if row.kind === 'Firmware'}
                    {row.locale}
                  {:else}
                    {row.titleId}{row.systemVersion ? ` · FW ${row.systemVersion}` : ''}
                  {/if}
                </td>
                <td class="text-muted">{formatSize(row.size)}</td>
                <td>
                  <button
                    onclick={() => enqueueSingle(row)}
                    disabled={isQueued(row) || downloaded(row)}
                    class="btn btn-secondary w-24 justify-center"
                  >
                    {downloaded(row) ? 'Downloaded' : isQueued(row) ? 'In progress' : 'Download'}
                  </button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>
  </section>

  <section class="panel flex min-h-0 flex-col overflow-hidden">
    <div class="panel-head">
      <div>
        <h2>Emulator</h2>
        <p>{mode === 'ps3' ? 'Configured paths and install actions' : 'No actions for this platform'}</p>
      </div>
      {#if mode === 'ps3'}
        <div class="flex flex-wrap items-center justify-end gap-2">
          <button
            onclick={syncAllNeeded}
            disabled={syncingAll || isMissingGamesConfig()}
            class="btn btn-primary"
            title="Make the PS3 title library exactly match the titles and updates listed by RPCS3 and PSN"
          >
            {syncingAll ? 'Syncing all' : 'Sync all'}
          </button>
          <button
            onclick={installFinishedPS3Jobs}
            disabled={installingDone || finishedPS3Jobs.length === 0 || isMissingInstallConfig()}
            class="btn btn-secondary"
            title="Install all completed PS3 update downloads into RPCS3"
          >
            {installingDone ? 'Installing downloads' : 'Install downloads'}
          </button>
          <button
            onclick={installPKGFolder}
            disabled={installingFolder || isMissingInstallConfig()}
            class="btn btn-secondary"
            title="Choose a folder of PS3 pkg files and install them into RPCS3"
          >
            {installingFolder ? 'Installing folder' : 'Install pkg folder'}
          </button>
          <button onclick={refreshEmulator} disabled={emulatorState.loading || isMissingGamesConfig()} class="btn btn-secondary">
            Refresh
          </button>
        </div>
      {/if}
    </div>

    {#if mode !== 'ps3'}
      <div class="empty">No emulator actions for {mode.toUpperCase()}</div>
    {:else if !emulatorState.cfg}
      <div class="empty">Loading emulator settings</div>
    {:else}
      {#if isMissingGamesConfig() || isMissingInstallConfig() || emulatorState.loadError || emulatorError}
        <div class="border-b border-error/40 bg-error-bg px-3 py-2 text-xs text-error-fg">
          {#if !emulatorState.gamesYMLInput}<div>Invalid setting: games.yml</div>{/if}
          {#if !emulatorState.hdd0Input}<div>Invalid setting: dev_hdd0/game</div>{/if}
          {#if !isMissingGamesConfig() && (emulatorState.loadError || emulatorError)}
            <div>{emulatorState.loadError || emulatorError}</div>
          {/if}
        </div>
      {/if}

      <div class="min-h-0 flex-1 overflow-auto">
        {#if emulatorState.loading}
          <div class="empty">Reconciling emulator library</div>
        {:else if emulatorState.rows.length === 0}
          <div class="empty">Emulator titles</div>
        {:else}
          <table class="data-table table-fixed" style={emulatorTableMinWidth ? `min-width: ${emulatorTableMinWidth}px` : undefined}>
            <colgroup>
              {#each emulatorColumnWidths as width, index (index)}
                <col style={`width: ${width}px`} />
              {/each}
            </colgroup>
            <thead>
              <tr>
                {#each EMULATOR_COLUMN_LABELS as label, index (label)}
                  <th>
                    {label}
                    <button
                      type="button"
                      class="column-resizer"
                      aria-label={`Resize ${label} column`}
                      onpointerdown={(event) => resizeColumn(event, 'emulator', index)}
                      onkeydown={(event) => resizeColumnWithKeyboard(event, 'emulator', index)}
                    ></button>
                  </th>
                {/each}
              </tr>
            </thead>
            <tbody>
              {#each emulatorState.rows as row (row.title_id)}
                <tr>
                  <td>
                    <div class="max-w-48 truncate" title={row.install_dir}>{row.name || row.title_id}</div>
                    <div class="font-mono text-xs text-muted-soft">{row.title_id}</div>
                  </td>
                  <td>
                    <span class="rounded px-2 py-0.5 text-xs {STATUS_BADGE[row.status] ?? 'bg-surface-2 text-muted'}" title={row.error}>
                      {STATUS_LABEL[row.status] ?? row.status}
                      {#if row.update_count > 0} ({row.downloaded_count}/{row.update_count}){/if}
                    </span>
                  </td>
                  <td>
                    <button
                      onclick={() => syncTitle(row.title_id)}
                      disabled={row.downloaded_count >= row.update_count || titleDownloadInProgress(row.title_id)}
                      class="btn btn-secondary w-20 justify-center"
                    >
                      {titleDownloadInProgress(row.title_id) ? 'Syncing' : 'Sync'}
                    </button>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
      </div>
    {/if}
  </section>

  <section class="panel flex min-h-0 flex-col overflow-hidden">
    <div class="panel-head">
      <div>
        <h2>Library</h2>
        <p>{mode.toUpperCase()} downloaded firmware and title updates</p>
      </div>
      <div class="flex items-center gap-2">
        <button onclick={deleteSelectedLibrary} disabled={selectedLibraryCount === 0 || libraryState.deleting} class="btn btn-secondary">
          Delete
        </button>
      </div>
    </div>

    {#if libraryState.error}
      <div class="mx-3 mt-3 rounded border border-error/40 bg-error-bg px-3 py-2 text-xs text-error-fg">
        {libraryState.error}
      </div>
    {/if}

    <div class="min-h-0 flex-1 overflow-auto">
        {#if libraryState.loading && modeLibraryTitles.length === 0}
          <div class="empty">Loading library</div>
        {:else if modeLibraryTitles.length === 0}
          <div class="empty">Downloaded firmware and title updates</div>
        {:else}
          <div class="divide-y divide-border text-xs" role="tree" aria-label="Downloaded library">
            {#if firmwareLibraryTitles.length > 0}
              {@const firmwareFiles = filesForTitles(firmwareLibraryTitles)}
              {@const firmwareKey = `branch:${mode}:firmware`}
              <section
                role="treeitem"
                aria-expanded={titleExpanded(firmwareKey)}
                aria-selected={fileSelectionState(firmwareFiles) !== 'none'}
              >
                <div class="tree-row bg-surface px-3">
                  <button
                    onclick={() => toggleTitleExpanded(firmwareKey)}
                    class="btn btn-quiet h-6 min-h-6 w-6 p-0"
                    aria-label={`${titleExpanded(firmwareKey) ? 'Collapse' : 'Expand'} firmware`}
                  >
                    <span aria-hidden="true">{titleExpanded(firmwareKey) ? '▾' : '▸'}</span>
                  </button>
                  <input
                    type="checkbox"
                    checked={fileSelectionState(firmwareFiles) === 'all'}
                    indeterminate={fileSelectionState(firmwareFiles) === 'some'}
                    aria-label={`Select ${mode} firmware`}
                    onchange={(event) => toggleFiles(firmwareFiles, event.currentTarget.checked)}
                  />
                  <span class="font-mono font-semibold text-fg">firmware</span>
                  <span class="text-muted">{firmwareLibraryTitles.length} locales · {formatSize(totalFileSize(firmwareFiles))}</span>
                  <span></span>
                </div>

                {#if titleExpanded(firmwareKey)}
                  <div class="divide-y divide-border" role="group">
                    {#each firmwareLibraryTitles as firmwareLocale (`${firmwareLocale.mode}-firmware-${firmwareLocale.locale}`)}
                      <section
                        role="treeitem"
                        aria-expanded={titleExpanded(firmwareLocale.path)}
                        aria-selected={titleSelectionState(firmwareLocale) !== 'none'}
                      >
                        <div class="tree-row bg-surface pl-7 pr-3">
                          <button
                            onclick={() => toggleTitleExpanded(firmwareLocale.path)}
                            class="btn btn-quiet h-6 min-h-6 w-6 p-0"
                            aria-label={`${titleExpanded(firmwareLocale.path) ? 'Collapse' : 'Expand'} ${firmwareLocale.locale}`}
                          >
                            <span aria-hidden="true">{titleExpanded(firmwareLocale.path) ? '▾' : '▸'}</span>
                          </button>
                          <input
                            type="checkbox"
                            checked={titleSelectionState(firmwareLocale) === 'all'}
                            indeterminate={titleSelectionState(firmwareLocale) === 'some'}
                            aria-label={`Select ${firmwareLocale.locale} firmware`}
                            onchange={(event) => toggleTitle(firmwareLocale, event.currentTarget.checked)}
                          />
                          <div class="min-w-0">
                            <div class="font-semibold text-fg-strong">{firmwareLocale.locale || 'unknown'}</div>
                            <div class="truncate font-mono text-muted-soft" title={firmwareLocale.path}>{firmwareLocale.path}</div>
                          </div>
                          <span class="text-muted">{firmwareLocale.file_count} files · {formatSize(firmwareLocale.total_size)}</span>
                          <button onclick={() => OpenFolder(firmwareLocale.path)} class="btn btn-secondary">Open</button>
                        </div>

                        {#if titleExpanded(firmwareLocale.path)}
                          <div class="divide-y divide-border bg-inset" role="group">
                            {#each firmwareLocale.files ?? [] as file (file.path)}
                              <div class="tree-file-row pl-12 pr-3" role="treeitem" aria-selected={selected(file.path)}>
                                <span class="text-center text-muted-faint" aria-hidden="true">└</span>
                                <input
                                  type="checkbox"
                                  checked={selected(file.path)}
                                  aria-label={`Select ${file.name}`}
                                  onchange={(event) => setSelected(file.path, event.currentTarget.checked)}
                                />
                                <div class="min-w-0">
                                  <div class="truncate text-fg" title={file.name}>{file.name}</div>
                                  <div class="truncate font-mono text-muted-soft" title={file.path}>{file.path}</div>
                                </div>
                                <div class="text-right text-muted">{labelForFile(file)}</div>
                              </div>
                            {/each}
                          </div>
                        {/if}
                      </section>
                    {/each}
                  </div>
                {/if}
              </section>
            {/if}

            {#if gameLibraryTitles.length > 0}
              {@const titleFiles = filesForTitles(gameLibraryTitles)}
              {@const titleKey = `branch:${mode}:title`}
              <section
                role="treeitem"
                aria-expanded={titleExpanded(titleKey)}
                aria-selected={fileSelectionState(titleFiles) !== 'none'}
              >
                <div class="tree-row bg-surface px-3">
                  <button
                    onclick={() => toggleTitleExpanded(titleKey)}
                    class="btn btn-quiet h-6 min-h-6 w-6 p-0"
                    aria-label={`${titleExpanded(titleKey) ? 'Collapse' : 'Expand'} title`}
                  >
                    <span aria-hidden="true">{titleExpanded(titleKey) ? '▾' : '▸'}</span>
                  </button>
                  <input
                    type="checkbox"
                    checked={fileSelectionState(titleFiles) === 'all'}
                    indeterminate={fileSelectionState(titleFiles) === 'some'}
                    aria-label={`Select ${mode} titles`}
                    onchange={(event) => toggleFiles(titleFiles, event.currentTarget.checked)}
                  />
                  <span class="font-mono font-semibold text-fg">title</span>
                  <span class="text-muted">{gameLibraryTitles.length} folders · {formatSize(totalFileSize(titleFiles))}</span>
                  <span></span>
                </div>

                {#if titleExpanded(titleKey)}
                  <div class="divide-y divide-border" role="group">
                    {#each gameLibraryTitles as title (`${title.mode}-${title.title_id}`)}
                      <section
                        role="treeitem"
                        aria-expanded={titleExpanded(title.path)}
                        aria-selected={titleSelectionState(title) !== 'none'}
                      >
                        <div class="tree-row bg-surface pl-7 pr-3">
                          <button
                            onclick={() => toggleTitleExpanded(title.path)}
                            class="btn btn-quiet h-6 min-h-6 w-6 p-0"
                            aria-label={`${titleExpanded(title.path) ? 'Collapse' : 'Expand'} ${title.title_id}`}
                          >
                            <span aria-hidden="true">{titleExpanded(title.path) ? '▾' : '▸'}</span>
                          </button>
                          <input
                            type="checkbox"
                            checked={titleSelectionState(title) === 'all'}
                            indeterminate={titleSelectionState(title) === 'some'}
                            aria-label={`Select ${title.title_id}`}
                            onchange={(event) => toggleTitle(title, event.currentTarget.checked)}
                          />
                          <div class="min-w-0">
                            <div class="font-semibold text-fg-strong">{title.title_id}</div>
                            <div class="truncate font-mono text-muted-soft" title={title.path}>{title.path}</div>
                          </div>
                          <span class="text-muted">{title.file_count} files · {formatSize(title.total_size)}</span>
                          <button onclick={() => OpenFolder(title.path)} class="btn btn-secondary">Open</button>
                        </div>

                        {#if titleExpanded(title.path)}
                          <div class="divide-y divide-border bg-inset" role="group">
                            {#each title.files ?? [] as file (file.path)}
                              <div class="tree-file-row pl-12 pr-3" role="treeitem" aria-selected={selected(file.path)}>
                                <span class="text-center text-muted-faint" aria-hidden="true">└</span>
                                <input
                                  type="checkbox"
                                  checked={selected(file.path)}
                                  aria-label={`Select ${file.name}`}
                                  onchange={(event) => setSelected(file.path, event.currentTarget.checked)}
                                />
                                <div class="min-w-0">
                                  <div class="truncate text-fg" title={file.name}>{file.name}</div>
                                  <div class="truncate font-mono text-muted-soft" title={file.path}>{file.path}</div>
                                </div>
                                <div class="text-right text-muted">{labelForFile(file)}</div>
                              </div>
                            {/each}
                          </div>
                        {/if}
                      </section>
                    {/each}
                  </div>
                {/if}
              </section>
            {/if}
          </div>
        {/if}
    </div>
  </section>

  <section class="panel flex min-h-0 flex-col overflow-hidden">
    <Activity embedded />
  </section>
</div>

<style>
  .workbench {
    display: grid;
    grid-template-columns: minmax(24rem, 1fr) minmax(24rem, 1fr);
    grid-template-rows: minmax(17rem, 1fr) minmax(17rem, 1fr);
    gap: 0.75rem;
    min-height: 0;
    height: 100%;
  }

  .panel-head {
    display: flex;
    min-height: 3.5rem;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    border-bottom: 1px solid var(--c-border);
    padding: 0.75rem;
  }

  .panel-head h2 {
    color: var(--c-fg-strong);
    font-size: 0.95rem;
    font-weight: 650;
    line-height: 1.2;
  }

  .panel-head p {
    color: var(--c-muted-soft);
    font-size: 0.75rem;
    line-height: 1.2;
    margin-top: 0.15rem;
  }

  table {
    border-collapse: collapse;
  }

  .data-table {
    width: 100%;
    max-width: 100%;
    font-size: 0.75rem;
  }

  .header-control {
    font-family: inherit;
    font-size: 0.75rem !important;
    line-height: 1rem;
  }

  th {
    background: var(--c-surface-2);
    color: var(--c-muted);
    font-size: 0.72rem;
    font-weight: 550;
    padding: 0.5rem 0.75rem;
    text-align: left;
    position: sticky;
    top: 0;
    z-index: 1;
  }

  .column-resizer {
    position: absolute;
    top: 0;
    right: 0;
    z-index: 2;
    width: 0.5rem;
    height: 100%;
    cursor: col-resize;
    touch-action: none;
    background: transparent;
  }

  .column-resizer::after {
    content: '';
    position: absolute;
    top: 25%;
    bottom: 25%;
    left: calc(50% - 0.5px);
    width: 1px;
    background: var(--c-border);
    opacity: 0;
  }

  .column-resizer:hover::after,
  .column-resizer:focus-visible::after {
    opacity: 1;
    background: var(--c-accent);
  }

  td {
    border-top: 1px solid var(--c-border);
    padding: 0.55rem 0.75rem;
    vertical-align: middle;
  }

  tbody tr:hover {
    background: var(--c-surface-2);
  }

  .tree-row {
    display: grid;
    min-height: 2.25rem;
    grid-template-columns: 1.5rem auto minmax(0, 1fr) auto auto;
    align-items: center;
    gap: 0.5rem;
    padding-top: 0.375rem;
    padding-bottom: 0.375rem;
  }

  .tree-file-row {
    display: grid;
    min-height: 2.25rem;
    grid-template-columns: 1.5rem auto minmax(0, 1fr) auto;
    align-items: center;
    gap: 0.5rem;
    padding-top: 0.375rem;
    padding-bottom: 0.375rem;
  }

  .empty {
    color: var(--c-muted-soft);
    font-size: 0.875rem;
    padding: 0.75rem;
  }

  @media (max-width: 900px) {
    .workbench {
      grid-template-columns: 1fr;
      grid-template-rows: minmax(18rem, auto) minmax(18rem, auto) minmax(18rem, 1fr);
      overflow: auto;
    }

    .library-panel {
      grid-column: auto;
    }
  }
</style>
