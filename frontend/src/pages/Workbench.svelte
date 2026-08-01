<script lang="ts">
  import { onMount, untrack } from 'svelte'
  import type { Mode } from '../app/types'
  import {
    AutoDetectGamesYML,
    DeleteLibraryItems,
    EnqueueDownload,
    InstallLibraryPKGsPS3,
    ListDownloadLibrary,
    ListRPCS3Library,
    OpenFolder,
    PendingLibraryPKGsPS3,
    ReconcileTitlePS3,
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
  import * as library from '../../bindings/PSNWDL/internal/library'
  import * as rpcs3 from '../../bindings/PSNWDL/internal/rpcs3'
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
  const initialMode = untrack(() => mode)
  const initialDefaultDownload = untrack(() => defaultDownload)

  let sourceByMode = $state<Record<Mode, Source>>({
    ps3: normalizedSource(initialDefaultDownload, 'ps3'),
    ps4: normalizedSource(initialDefaultDownload, 'ps4'),
    ps5: 'firmware',
    psvita: normalizedSource(initialDefaultDownload, 'psvita'),
  })
  let source = $state<Source>(sourceByMode[initialMode])
  let downloadError = $state<string | null>(null)
  let emulatorError = $state<string | null>(null)
  let syncingAll = $state(false)
  let emulatorSyncJobIDs = $state<string[]>([])
  let installingAll = $state(false)
  let pendingPKGCount = $state(0)
  let pendingPKGError = $state<string | null>(null)
  let pendingPKGGeneration = 0
  let includeDRMFree = $state(false)
  let lastMode = $state<Mode>(initialMode)
  let emulatorRefreshing = $state(false)
  let emulatorRefreshGeneration = 0
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
  let downloadColumnLabels = $derived(['Kind', 'Version', source === 'firmware' ? 'Region' : 'Scope', 'Size', 'Action'])
  let firmwareLoading = $derived($fetching === mode && !cachedFirmware?.list)
  let emulatorSyncActive = $derived(emulatorSyncJobIDs.length > 0)
  let selectedLibraryCount = $derived(libraryState.selected.length)
  $effect(() => {
    if (mode !== lastMode) {
      sourceByMode[lastMode] = source
      source = sourceByMode[mode]
      libraryState.selected = []
      lastMode = mode
      void refreshLibrary()
    }
    downloadError = null
  })

  onMount(async () => {
    if (!libraryState.initialized) {
      libraryState.initialized = true
      await refreshLibrary()
    }
    const firstEmulatorLoad = !emulatorState.initialized
    await ensureEmulatorBooted()
    if (firstEmulatorLoad) await refreshEmulator(true)
  })

  $effect(() => {
    if (!emulatorState.initialized) return
    hydrateEmulatorConfig(appConfig)
    void refreshPendingPKGs()
  })

  $effect(() => {
    const offs = [
      Events.On('downloads:updated', ({ data: next }) => {
        libraryState.titles = Array.isArray(next) ? next : []
        libraryState.loading = false
        pruneLibrarySelection()
        refreshCachedEmulatorRows()
        void refreshPendingPKGs()
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
          region: entry.region,
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
    region?: string
    type?: string
    systemVersion?: string
    update: psn.Update
  }

  let modeLibraryTitles = $derived(libraryState.titles.filter((title) => title.mode === mode))
  let firmwareLibraryRegions = $derived(modeLibraryTitles.filter((title) => title.title_id === 'firmware'))
  let gameLibraryTitles = $derived(modeLibraryTitles.filter((title) => title.title_id !== 'firmware'))
  let firmwareLibraryRoot = $derived(firmwareLibraryRegions.length > 0 ? parentPath(firmwareLibraryRegions[0].path) : '')
  let titleLibraryRoot = $derived(gameLibraryTitles.length > 0 ? parentPath(gameLibraryTitles[0].path) : '')

  async function ensureEmulatorBooted() {
    if (emulatorState.initialized) return
    hydrateEmulatorConfig(appConfig)
    emulatorState.detectedPath = await AutoDetectGamesYML()
    emulatorState.initialized = true
  }

  function hydrateEmulatorConfig(next: config.Config) {
    emulatorState.cfg = next
    emulatorState.gamesYMLInput = next.rpcs3.games_yml
    emulatorState.hdd0Input = next.rpcs3.hdd0_game
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
    if (!canSourceSearch) return
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
      region: row.region || '',
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
      refreshCachedEmulatorRows()
      void refreshPendingPKGs()
    } catch (e) {
      libraryState.error = e instanceof Error ? e.message : String(e)
      libraryState.titles = []
    } finally {
      libraryState.loading = false
    }
  }

  async function deleteSelectedLibrary() {
    if (libraryState.selected.length === 0 || libraryState.deleting) return
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

  async function refreshEmulator(initialLoad = false) {
    if (emulatorRefreshing || (mode !== 'ps3' && !initialLoad)) return
    if (isMissingGamesConfig()) {
      emulatorState.rows = []
      emulatorState.loadError = null
      return
    }
    const generation = ++emulatorRefreshGeneration
    emulatorRefreshing = true
    emulatorState.loading = emulatorState.rows.length === 0
    emulatorState.loadError = null
    emulatorError = null
    try {
      const entries = (await ListRPCS3Library()) ?? []
      if (generation !== emulatorRefreshGeneration) return
      emulatorState.rows = entries.map(
        (entry) =>
          new library.Row({
            title_id: entry.title_id,
            install_dir: entry.install_dir,
            status: library.Status.StatusChecking,
            downloaded_count: 0,
            update_count: 0,
          })
      )
      emulatorState.loading = false
      await Promise.all(
        entries.map(async (entry) => {
          try {
            const row = await ReconcileTitlePS3(entry)
            if (generation !== emulatorRefreshGeneration) return
            emulatorState.rows = emulatorState.rows.map((current) =>
              current.title_id === row.title_id ? applyLibraryStateToRow(row) : current
            )
          } catch (e) {
            if (generation !== emulatorRefreshGeneration) return
            emulatorState.rows = emulatorState.rows.map((current) =>
              current.title_id === entry.title_id
                ? new library.Row({
                    ...current,
                    status: library.Status.StatusUnreachable,
                    error: e instanceof Error ? e.message : String(e),
                  })
                : current
            )
          }
        })
      )
    } catch (e) {
      if (generation === emulatorRefreshGeneration) {
        emulatorState.rows = []
        emulatorState.loadError = e instanceof Error ? e.message : String(e)
      }
    } finally {
      if (generation === emulatorRefreshGeneration) {
        emulatorState.loading = false
        emulatorRefreshing = false
      }
    }
  }

  async function syncTitle(titleID: string) {
    if (titleDownloadInProgress(titleID)) return
    emulatorError = null
    try {
      const jobIDs = (await SyncTitlePS3(titleID)) ?? []
      trackEmulatorJobs(jobIDs)
    } catch (e) {
      emulatorError = e instanceof Error ? e.message : String(e)
    }
  }

  async function syncAllNeeded() {
    if (!canSyncAll()) return
    syncingAll = true
    emulatorError = null
    try {
      const entries = emulatorState.rows.map(
        (row) => new rpcs3.Entry({ title_id: row.title_id, install_dir: row.install_dir })
      )
      const jobIDs = (await SyncAllPS3(entries)) ?? []
      trackEmulatorJobs(jobIDs)
    } catch (e) {
      emulatorError = e instanceof Error ? e.message : String(e)
    } finally {
      syncingAll = false
    }
  }

  async function installAll() {
    if (installingAll || pendingPKGCount === 0 || isMissingInstallConfig()) return
    installingAll = true
    emulatorError = null
    try {
      await InstallLibraryPKGsPS3()
    } catch (e) {
      emulatorError = e instanceof Error ? e.message : String(e)
    } finally {
      await refreshPendingPKGs()
      installingAll = false
    }
  }

  async function refreshPendingPKGs() {
    const generation = ++pendingPKGGeneration
    if (!emulatorState.initialized || isMissingInstallConfig()) {
      pendingPKGCount = 0
      pendingPKGError = null
      return
    }
    try {
      const count = await PendingLibraryPKGsPS3()
      if (generation !== pendingPKGGeneration) return
      pendingPKGCount = count
      pendingPKGError = null
    } catch (e) {
      if (generation !== pendingPKGGeneration) return
      pendingPKGCount = 0
      pendingPKGError = e instanceof Error ? e.message : String(e)
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
        (row.kind !== 'Firmware' || title.region === row.region)
    )
    if (!cachedTitle) return false
    const version = row.kind === 'DRM-free' ? `${row.version}_drm_free` : row.version
    return (cachedTitle.files ?? []).some((file) => file.version === version)
  }

  function applyLibraryStateToRow(row: library.Row): library.Row {
    const updates = row.updates ?? []
    if (updates.length === 0 && row.status !== library.Status.StatusNone) return row
    const uniqueUpdates = [...new Map(updates.map((update) => [update.version, update])).values()]
    const localFiles =
      libraryState.titles.find((title) => title.mode === 'ps3' && title.title_id === row.title_id)?.files ?? []
    const downloadedCount = uniqueUpdates.filter((update) =>
      localFiles.some((file) => file.version === update.version && (!update.size || file.size === update.size))
    ).length
    return new library.Row({
      ...row,
      downloaded_count: downloadedCount,
      update_count: uniqueUpdates.length,
      status: statusForDownloadCounts(downloadedCount, uniqueUpdates.length),
    })
  }

  function refreshCachedEmulatorRows() {
    if (emulatorState.rows.length === 0) return
    emulatorState.rows = emulatorState.rows.map(applyLibraryStateToRow)
  }

  function statusForDownloadCounts(downloadedCount: number, updateCount: number): library.Status {
    if (updateCount === 0) return library.Status.StatusNone
    if (downloadedCount === 0) return library.Status.StatusNoneDownloaded
    if (downloadedCount < updateCount) return library.Status.StatusSomeDownloaded
    return library.Status.StatusAllDownloaded
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

  function canSyncRow(row: library.Row): boolean {
    return row.status !== library.Status.StatusChecking &&
      row.status !== library.Status.StatusUnreachable &&
      row.downloaded_count < row.update_count
  }

  function canSyncAll(): boolean {
    return !syncingAll &&
      !emulatorSyncActive &&
      !emulatorRefreshing &&
      !isMissingGamesConfig() &&
      !emulatorState.loadError &&
      emulatorState.rows.some(canSyncRow)
  }

  function syncActionLabel(row: library.Row): string {
    if (titleDownloadInProgress(row.title_id)) return 'Syncing'
    if (row.status === library.Status.StatusUnreachable) return 'Blocked'
    if (row.status === library.Status.StatusAllDownloaded || row.status === library.Status.StatusNone) return 'Synced'
    return 'Sync'
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
    return `${entry.region}-${entry.type || 'Firmware'}-${entry.version}-${entry.url}`
  }

  function labelForFile(file: File): string {
    const version = file.version ? `v${file.version}` : 'unknown version'
    return `${version} · ${formatSize(file.size)}`
  }

  function parentPath(path: string): string {
    const index = Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\'))
    return index > 0 ? path.slice(0, index) : path
  }

  const STATUS_BADGE: Record<string, string> = {
    checking: 'bg-surface-2 text-muted',
    all_downloaded: 'bg-success-bg text-success-fg',
    some_downloaded: 'bg-warn-bg text-warn-fg',
    none_downloaded: 'bg-error-bg text-error-fg',
    none: 'bg-success-bg text-success-fg',
    unreachable: 'bg-error-bg text-error-fg',
  }

  const STATUS_LABEL: Record<string, string> = {
    checking: 'Checking',
    all_downloaded: 'All downloaded',
    some_downloaded: 'Some downloaded',
    none_downloaded: 'None downloaded',
    none: 'None found',
    unreachable: 'Server unreachable',
  }
</script>

<div class="workbench">
  <section class="downloader-panel panel flex min-h-0 flex-col overflow-hidden">
    <div class="panel-head">
      <div>
        <h2>Downloader</h2>
        <p>{mode.toUpperCase()} discovery and queueing</p>
      </div>
      <form
        class="flex min-w-0 flex-wrap items-center justify-end gap-2"
        onsubmit={(e) => {
          e.preventDefault()
          refreshSource()
        }}
      >
        <label class="flex w-28 items-center gap-1 text-xs text-muted" class:invisible={source !== 'title' || mode !== 'ps3'}>
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
                    {row.region}
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
                    {downloaded(row) ? 'Downloaded' : isQueued(row) ? 'Downloading' : 'Download'}
                  </button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>
  </section>

  <section class="emulator-panel panel flex min-h-0 flex-col overflow-hidden">
    <div class="panel-head">
      <div>
        <h2>Emulator</h2>
        <p>{mode === 'ps3' ? 'Configured paths and install actions' : 'No actions for this platform'}</p>
      </div>
      {#if mode === 'ps3'}
        <div class="flex flex-wrap items-center justify-end gap-2">
          <button
            onclick={syncAllNeeded}
            disabled={!canSyncAll()}
            class="btn btn-primary"
          >
            Sync all
          </button>
          <button
            onclick={installAll}
            disabled={installingAll || pendingPKGCount === 0 || isMissingInstallConfig()}
            class="btn btn-secondary"
          >
            Install all
          </button>
          <button onclick={() => refreshEmulator()} disabled={emulatorRefreshing || isMissingGamesConfig()} class="btn btn-secondary">
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
      {#if isMissingGamesConfig() || isMissingInstallConfig() || emulatorState.loadError || emulatorError || pendingPKGError}
        <div class="border-b border-error/40 bg-error-bg px-3 py-2 text-xs text-error-fg">
          {#if !emulatorState.gamesYMLInput}<div>Invalid setting: games.yml</div>{/if}
          {#if !emulatorState.hdd0Input}<div>Invalid setting: dev_hdd0/game</div>{/if}
          {#if !isMissingGamesConfig() && (emulatorState.loadError || emulatorError)}
            <div>{emulatorState.loadError || emulatorError}</div>
          {/if}
          {#if pendingPKGError}<div>{pendingPKGError}</div>{/if}
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
                      disabled={!canSyncRow(row) || titleDownloadInProgress(row.title_id)}
                      class="btn btn-secondary w-20 justify-center"
                    >
                      {syncActionLabel(row)}
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

  <section class="library-panel panel flex min-h-0 flex-col overflow-hidden">
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
            {#if firmwareLibraryRegions.length > 0}
              {@const firmwareFiles = filesForTitles(firmwareLibraryRegions)}
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
                  <div class="min-w-0">
                    <div class="font-mono font-semibold text-fg">firmware</div>
                    <div class="truncate font-mono text-muted-soft" title={firmwareLibraryRoot}>{firmwareLibraryRoot}</div>
                  </div>
                  <span class="text-muted">{firmwareLibraryRegions.length} regions · {formatSize(totalFileSize(firmwareFiles))}</span>
                  <button onclick={() => OpenFolder(firmwareLibraryRoot)} class="btn btn-secondary">Open</button>
                </div>

                {#if titleExpanded(firmwareKey)}
                  <div class="divide-y divide-border" role="group">
                    {#each firmwareLibraryRegions as firmwareRegion (`${firmwareRegion.mode}-firmware-${firmwareRegion.region}`)}
                      <section
                        role="treeitem"
                        aria-expanded={titleExpanded(firmwareRegion.path)}
                        aria-selected={titleSelectionState(firmwareRegion) !== 'none'}
                      >
                        <div class="tree-row bg-surface pl-7 pr-3">
                          <button
                            onclick={() => toggleTitleExpanded(firmwareRegion.path)}
                            class="btn btn-quiet h-6 min-h-6 w-6 p-0"
                            aria-label={`${titleExpanded(firmwareRegion.path) ? 'Collapse' : 'Expand'} ${firmwareRegion.region}`}
                          >
                            <span aria-hidden="true">{titleExpanded(firmwareRegion.path) ? '▾' : '▸'}</span>
                          </button>
                          <input
                            type="checkbox"
                            checked={titleSelectionState(firmwareRegion) === 'all'}
                            indeterminate={titleSelectionState(firmwareRegion) === 'some'}
                            aria-label={`Select ${firmwareRegion.region} firmware`}
                            onchange={(event) => toggleTitle(firmwareRegion, event.currentTarget.checked)}
                          />
                          <div class="min-w-0">
                            <div class="font-semibold text-fg-strong">{firmwareRegion.region || 'unknown'}</div>
                            <div class="truncate font-mono text-muted-soft" title={firmwareRegion.path}>{firmwareRegion.path}</div>
                          </div>
                          <span class="text-muted">{firmwareRegion.file_count} files · {formatSize(firmwareRegion.total_size)}</span>
                          <button onclick={() => OpenFolder(firmwareRegion.path)} class="btn btn-secondary">Open</button>
                        </div>

                        {#if titleExpanded(firmwareRegion.path)}
                          <div class="divide-y divide-border bg-inset" role="group">
                            {#each firmwareRegion.files ?? [] as file (file.path)}
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
                  <div class="min-w-0">
                    <div class="font-mono font-semibold text-fg">title</div>
                    <div class="truncate font-mono text-muted-soft" title={titleLibraryRoot}>{titleLibraryRoot}</div>
                  </div>
                  <span class="text-muted">{gameLibraryTitles.length} folders · {formatSize(totalFileSize(titleFiles))}</span>
                  <button onclick={() => OpenFolder(titleLibraryRoot)} class="btn btn-secondary">Open</button>
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

  <section class="activity-panel panel flex min-h-0 flex-col overflow-hidden">
    <Activity embedded />
  </section>
</div>

<style>
  .workbench {
    display: grid;
    grid-template-columns: minmax(24rem, 1fr) minmax(24rem, 1fr);
    grid-template-rows: minmax(17rem, 1fr) minmax(17rem, 1fr);
    grid-template-areas:
      'downloader activity'
      'library emulator';
    gap: 0.75rem;
    min-height: 0;
    height: 100%;
  }

  .downloader-panel {
    grid-area: downloader;
  }

  .activity-panel {
    grid-area: activity;
  }

  .emulator-panel {
    grid-area: emulator;
  }

  .library-panel {
    grid-area: library;
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
      grid-template-rows: repeat(4, minmax(18rem, auto));
      grid-template-areas:
        'downloader'
        'activity'
        'library'
        'emulator';
      overflow: auto;
    }
  }
</style>
