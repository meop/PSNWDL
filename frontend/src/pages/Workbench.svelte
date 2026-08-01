<script lang="ts">
  import { onMount } from 'svelte'
  import type { Mode } from '../app/types'
  import {
    AutoDetectGamesYML,
    ClearTitleCache,
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
    SyncTitlePS3,
    UpdateDownloadLibrary,
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

  let { mode, defaultDownload = 'firmware', appConfig }: Props = $props()

  let source = $state<Source>('firmware')
  let selectedDownloadBuckets = $state<Record<string, string[]>>({})
  let downloadError = $state<string | null>(null)
  let emulatorError = $state<string | null>(null)
  let syncingAll = $state(false)
  let emulatorSyncJobIDs = $state<string[]>([])
  let installingDone = $state(false)
  let installingFolder = $state(false)
  let includeDRMFree = $state(false)
  let lastMode = $state<Mode>('ps3')
  let emulatorConfigKey = $state('')
  let expandedLibraryTitles = $state<Record<string, boolean>>({})
  let titleState = $derived(searchState[mode])
  let normalizedID = $derived(titleState.titleID.trim().toUpperCase())
  let canSearch = $derived(/^[A-Z]{4}\d{5}$/.test(normalizedID) && !titleState.loading && mode !== 'ps5')
  let canSourceSearch = $derived(source === 'firmware' ? $fetching !== mode : canSearch)
  let cachedFirmware = $derived($cache[mode] ?? null)
  let firmwareLoading = $derived($fetching === mode && !cachedFirmware?.list)
  let downloadSelectionKey = $derived(
    `${mode}:${source}:${source === 'title' ? titleState.result?.id || normalizedID || 'empty' : 'firmware'}`
  )
  let selectedDownloads = $derived(selectedDownloadBuckets[downloadSelectionKey] ?? [])
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
    if (nextKey === emulatorConfigKey) return
    hydrateEmulatorConfig(appConfig)
    emulatorConfigKey = nextKey
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

  let downloadRows = $derived.by(() => {
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
  let availableDownloadRows = $derived(downloadRows.filter((row) => !isQueued(row)))
  let selectedAvailableCount = $derived(
    availableDownloadRows.filter((row) => selectedDownloads.includes(row.key)).length
  )

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
    return `${next.rpcs3.games_yml}\0${next.rpcs3.hdd0_game}\0${next.storage.library_dir}`
  }

  async function searchTitle() {
    if (!canSearch) return
      titleState.loading = true
      titleState.error = null
      titleState.result = null
      selectedDownloadBuckets[downloadSelectionKey] = []
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

  async function enqueueSelected() {
    const selected = downloadRows.filter((row) => selectedDownloads.includes(row.key) && !isQueued(row))
    if (selected.length === 0) return
    downloadError = null
    try {
      for (const row of selected) {
        await enqueueRow(row)
      }
      selectedDownloadBuckets[downloadSelectionKey] = []
    } catch (e) {
      downloadError = e instanceof Error ? e.message : String(e)
    }
  }

  async function enqueueSingle(row: DownloadRow) {
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

  async function updateLibrary() {
    libraryState.updating = true
    libraryState.error = null
    try {
      libraryState.titles = (await UpdateDownloadLibrary()) ?? []
      pruneLibrarySelection()
    } catch (e) {
      libraryState.error = e instanceof Error ? e.message : String(e)
    } finally {
      libraryState.updating = false
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
    if (isMissingEmulatorConfig()) {
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
    const needed = emulatorState.rows.filter((row) => row.status === 'update_available' || row.status === 'missing_all')
    if (needed.length === 0) return
    syncingAll = true
    emulatorError = null
    try {
      const jobIDs: string[] = []
      for (const row of needed) {
        jobIDs.push(...((await SyncTitlePS3(row.title_id)) ?? []))
      }
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
    emulatorError = null
    try {
      for (const job of finishedPS3Jobs) {
        await InstallJob(job.id)
      }
      await refreshEmulator()
    } catch (e) {
      emulatorError = e instanceof Error ? e.message : String(e)
    } finally {
      installingDone = false
    }
  }

  async function clearCache(titleID: string) {
    emulatorError = null
    try {
      await ClearTitleCache(titleID)
      await refreshEmulator()
    } catch (e) {
      emulatorError = e instanceof Error ? e.message : String(e)
    }
  }

  async function installPKGFolder() {
    if (!emulatorState.cfg || isMissingEmulatorConfig()) return
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

  async function openCacheFolder(titleID: string) {
    const root = emulatorState.cfg?.storage.library_dir || ''
    await OpenFolder(`${root}/ps3/title/${titleID}`)
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

  function selectedDownload(key: string): boolean {
    return selectedDownloads.includes(key)
  }

  function setSelectedDownload(key: string, checked: boolean) {
    const current = selectedDownloads
    if (checked) {
      if (!current.includes(key)) selectedDownloadBuckets[downloadSelectionKey] = [...current, key]
      return
    }
    selectedDownloadBuckets[downloadSelectionKey] = current.filter((item) => item !== key)
  }

  function selectAllDownloads(checked: boolean) {
    selectedDownloadBuckets[downloadSelectionKey] = checked ? availableDownloadRows.map((row) => row.key) : []
  }

  function isMissingEmulatorConfig(): boolean {
    return !emulatorState.gamesYMLInput || !emulatorState.hdd0Input
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
    up_to_date: 'bg-success-bg text-success-fg',
    update_available: 'bg-warn-bg text-warn-fg',
    missing_all: 'bg-error-bg text-error-fg',
    cached_not_installed: 'bg-surface-3 text-fg',
    no_updates: 'bg-surface-2 text-muted',
    unreachable: 'bg-surface-2 text-muted-soft',
  }

  const STATUS_LABEL: Record<string, string> = {
    up_to_date: 'Up to date',
    update_available: 'Update available',
    missing_all: 'Missing all',
    cached_not_installed: 'Cached, not installed',
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
      <div class="flex items-center gap-2">
        <select bind:value={source} class="input h-8 px-2 text-xs">
          <option value="firmware">Firmware</option>
          {#if mode !== 'ps5'}<option value="title">Title</option>{/if}
        </select>
      </div>
    </div>

    <div class="border-b border-border p-3">
      <form
        class="flex items-center justify-between gap-3"
        onsubmit={(e) => {
          e.preventDefault()
          refreshSource()
        }}
      >
        <div class="min-w-0 text-sm text-muted">
          {#if source === 'title'}
            Search by title ID, for example BCUS98114
          {:else}
            Search latest firmware by region for {mode.toUpperCase()}
          {/if}
        </div>
        <div class="flex shrink-0 items-center gap-2">
          {#if source === 'title'}
            <input
              bind:value={titleState.titleID}
              placeholder="BCUS98114"
              maxlength="9"
              class="input h-9 w-36 px-3 font-mono text-sm"
            />
            {#if mode === 'ps3'}
              <label class="flex items-center gap-1 text-xs text-muted">
                <input type="checkbox" bind:checked={includeDRMFree} />
                DRM-free
              </label>
            {/if}
          {/if}
          <button type="submit" disabled={!canSourceSearch} class="btn btn-primary h-9 px-4">Search</button>
        </div>
      </form>
      {#if titleState.error || downloadError}
        <div class="mt-3 rounded border border-error/40 bg-error-bg px-3 py-2 text-xs text-error-fg">
          {titleState.error || downloadError}
        </div>
      {/if}
    </div>

    <div class="flex items-center justify-between border-b border-border px-3 py-2 text-xs text-muted-soft">
      <label class="flex items-center gap-2">
        <input
          type="checkbox"
          checked={availableDownloadRows.length > 0 && selectedAvailableCount === availableDownloadRows.length}
          onchange={(e) => selectAllDownloads(e.currentTarget.checked)}
        />
        Select all
        <span class="inline-block min-w-16 text-muted-faint">{selectedAvailableCount} selected</span>
      </label>
      <button onclick={enqueueSelected} disabled={selectedAvailableCount === 0} class="btn btn-primary">
        Download selected
      </button>
    </div>

    <div class="min-h-0 flex-1 overflow-auto">
      {#if firmwareLoading}
        <div class="empty">Loading latest firmware</div>
      {:else if downloadRows.length === 0}
        <div class="empty">{source === 'title' ? 'Title update results' : 'Latest firmware by region'}</div>
      {:else}
        <table class="w-full table-fixed text-sm">
          <thead>
            <tr>
              <th class="w-8"></th>
              <th class="w-24">Kind</th>
              <th class="w-24">Version</th>
              <th>Scope</th>
              <th class="w-24">Size</th>
              <th class="w-32">Action</th>
            </tr>
          </thead>
          <tbody>
            {#each downloadRows as row (row.key)}
              <tr>
                <td>
                  <input
                    type="checkbox"
                    checked={selectedDownload(row.key)}
                    disabled={isQueued(row)}
                    onchange={(e) => setSelectedDownload(row.key, e.currentTarget.checked)}
                  />
                </td>
                <td>{row.kind}</td>
                <td class="font-mono">v{row.version}</td>
                <td class="truncate text-muted">
                  {#if row.kind === 'Firmware'}
                    {row.locale} · {row.type}
                  {:else}
                    {row.titleId}{row.systemVersion ? ` · FW ${row.systemVersion}` : ''}
                  {/if}
                </td>
                <td class="text-muted">{formatSize(row.size)}</td>
                <td>
                  <button onclick={() => enqueueSingle(row)} disabled={isQueued(row)} class="btn btn-primary w-24 justify-center">
                    {isQueued(row) ? 'In progress' : 'Download'}
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
        <button onclick={refreshEmulator} disabled={emulatorState.loading || isMissingEmulatorConfig()} class="btn btn-secondary">
          Refresh
        </button>
      {/if}
    </div>

    {#if mode !== 'ps3'}
      <div class="empty">No emulator actions for {mode.toUpperCase()}</div>
    {:else if !emulatorState.cfg}
      <div class="empty">Loading emulator settings</div>
    {:else}
      <div class="border-b border-border p-3">
        {#if isMissingEmulatorConfig()}
          <div class="mb-3 rounded border border-error/40 bg-error-bg px-3 py-2 text-xs text-error-fg">
            {#if !emulatorState.gamesYMLInput}<div>Invalid setting: games.yml</div>{/if}
            {#if !emulatorState.hdd0Input}<div>Invalid setting: dev_hdd0/game</div>{/if}
          </div>
        {/if}
        <div class="flex flex-wrap justify-end gap-2">
          <button onclick={syncAllNeeded} disabled={syncingAll || emulatorState.rows.length === 0} class="btn btn-primary">
            Download updates
          </button>
          <button onclick={installFinishedPS3Jobs} disabled={installingDone || finishedPS3Jobs.length === 0} class="btn btn-secondary">
            Install completed
          </button>
          <button onclick={installPKGFolder} disabled={installingFolder || isMissingEmulatorConfig()} class="btn btn-secondary">
            {installingFolder ? 'Installing folder' : 'Install PKG folder'}
          </button>
        </div>
        {#if (emulatorState.loadError && !isMissingEmulatorConfig()) || emulatorError}
          <div class="mt-3 rounded border border-error/40 bg-error-bg px-3 py-2 text-xs text-error-fg">
            {emulatorState.loadError || emulatorError}
          </div>
        {/if}
      </div>

      <div class="min-h-0 flex-1 overflow-auto">
        {#if emulatorState.loading}
          <div class="empty">Reconciling emulator library</div>
        {:else if emulatorState.rows.length === 0}
          <div class="empty">Emulator titles</div>
        {:else}
          <table class="w-full table-fixed text-sm">
            <thead>
              <tr>
                <th>Title</th>
                <th>Installed -> Server</th>
                <th>Status</th>
                <th class="w-52">Action</th>
              </tr>
            </thead>
            <tbody>
              {#each emulatorState.rows as row (row.title_id)}
                <tr>
                  <td>
                    <div class="max-w-48 truncate" title={row.install_dir}>{row.name || row.title_id}</div>
                    <div class="font-mono text-xs text-muted-soft">{row.title_id}</div>
                  </td>
                  <td class="text-xs text-muted">
                    {row.installed_version || row.latest_local || '-'} -> {row.latest_server || '-'}
                  </td>
                  <td>
                    <span class="rounded px-2 py-0.5 text-xs {STATUS_BADGE[row.status] ?? 'bg-surface-2 text-muted'}" title={row.error}>
                      {STATUS_LABEL[row.status] ?? row.status}
                    </span>
                  </td>
                  <td>
                    <div class="flex gap-1">
                      {#if row.status === 'update_available' || row.status === 'missing_all'}
                        <button onclick={() => syncTitle(row.title_id)} disabled={titleDownloadInProgress(row.title_id)} class="btn btn-primary">
                          {titleDownloadInProgress(row.title_id) ? 'In progress' : 'Download'}
                        </button>
                      {/if}
                      {#if row.latest_local}
                        <button onclick={() => openCacheFolder(row.title_id)} class="btn btn-secondary">Open</button>
                        <button onclick={() => clearCache(row.title_id)} class="btn btn-secondary">Clear</button>
                      {/if}
                    </div>
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
        <p>Shared cache for downloads and installs</p>
      </div>
      <div class="flex items-center gap-2">
        <button onclick={updateLibrary} disabled={libraryState.loading || libraryState.updating} class="btn btn-primary">
          Check updates
        </button>
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
        {#if libraryState.loading && libraryState.titles.length === 0}
          <div class="empty">Loading library</div>
        {:else if libraryState.titles.length === 0}
          <div class="empty">Downloaded firmware and title updates</div>
        {:else}
          <div class="divide-y divide-border" role="tree" aria-label="Downloaded library">
            {#each libraryState.titles as title (`${title.mode}-${title.title_id}`)}
              <section
                role="treeitem"
                aria-expanded={titleExpanded(title.path)}
                aria-selected={titleSelectionState(title) !== 'none'}
              >
                <div class="grid grid-cols-[1.5rem_auto_1fr_auto_auto] items-center gap-2 bg-surface px-3 py-2">
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
                    onchange={(e) => toggleTitle(title, e.currentTarget.checked)}
                  />
                  <div class="min-w-0">
                    <div class="flex flex-wrap items-baseline gap-x-2">
                      <span class="font-semibold text-fg-strong">{title.title_id}</span>
                      <span class="rounded bg-surface-2 px-2 py-0.5 text-xs uppercase text-muted">{title.mode}</span>
                      {#if title.latest_version}<span class="text-xs text-muted">Latest v{title.latest_version}</span>{/if}
                    </div>
                    <div class="truncate font-mono text-xs text-muted-soft" title={title.path}>{title.path}</div>
                  </div>
                  <div class="text-right text-xs text-muted">
                    <div>{title.file_count} file{title.file_count === 1 ? '' : 's'}</div>
                    <div>{formatSize(title.total_size)}</div>
                  </div>
                  <button onclick={() => OpenFolder(title.path)} class="btn btn-secondary">Open</button>
                </div>

                {#if titleExpanded(title.path)}
                  <div class="divide-y divide-border bg-inset" role="group">
                    {#each title.files ?? [] as file (file.path)}
                      <div
                        class="grid grid-cols-[1.5rem_auto_1fr_auto] items-center gap-2 px-3 py-2 text-sm"
                        role="treeitem"
                        aria-selected={selected(file.path)}
                      >
                        <span class="text-center text-muted-faint" aria-hidden="true">└</span>
                        <input
                          type="checkbox"
                          checked={selected(file.path)}
                          aria-label={`Select ${file.name}`}
                          onchange={(e) => setSelected(file.path, e.currentTarget.checked)}
                        />
                        <div class="min-w-0">
                          <div class="truncate text-fg" title={file.name}>{file.name}</div>
                          <div class="truncate font-mono text-xs text-muted-soft" title={file.path}>{file.path}</div>
                        </div>
                        <div class="text-right text-xs text-muted">
                          <div>{file.kind || 'File'}</div>
                          <div>{labelForFile(file)}</div>
                        </div>
                      </div>
                    {/each}
                  </div>
                {/if}
              </section>
            {/each}
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

  td {
    border-top: 1px solid var(--c-border);
    padding: 0.55rem 0.75rem;
    vertical-align: middle;
  }

  tbody tr:hover {
    background: var(--c-surface-2);
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
