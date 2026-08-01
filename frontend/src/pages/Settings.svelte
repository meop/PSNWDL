<script lang="ts">
  import { onMount } from 'svelte'
  import {
    AutoDetectGamesYML,
    ConfigFilePath,
    GetConfig,
    PickDirectory,
    PickGamesYML,
    SaveConfig,
    ValidateSettingsPath,
  } from '../../bindings/PSNWDL/app'
  import * as config from '../../bindings/PSNWDL/internal/config'
  import { MODES } from '../app/types'
  import { theme } from '../app/theme.svelte'
  import Loading from '../components/Loading.svelte'
  import { libraryState as emulatorState } from '../app/libraryStore.svelte'

  let cfg = $state<config.Config | null>(null)
  let configPath = $state('')
  let libraryDirInput = $state('')
  let libraryDirError = $state('')
  let detectedGamesYML = $state('')
  let gamesYMLInput = $state('')
  let gamesYMLError = $state('')
  let hdd0Input = $state('')
  let hdd0Error = $state('')
  let maxConcurrent = $state(5)
  let retryCount = $state(3)

  onMount(async () => {
    await hydrateSettings()
  })

  async function hydrateSettings() {
    const [nextCfg, nextConfigPath, nextDetectedGamesYML] = await Promise.all([
      GetConfig(),
      ConfigFilePath(),
      AutoDetectGamesYML(),
    ])

    configPath = nextConfigPath
    libraryDirInput = nextCfg.storage.library_dir
    libraryDirError = ''
    gamesYMLInput = nextCfg.rpcs3.games_yml
    gamesYMLError = ''
    hdd0Input = nextCfg.rpcs3.hdd0_game
    hdd0Error = ''
    maxConcurrent = nextCfg.network.max_concurrent_downloads
    retryCount = nextCfg.network.retry_count
    detectedGamesYML = nextDetectedGamesYML
    cfg = nextCfg
  }

  async function applyConfig(next: config.Config) {
    await SaveConfig(next)
    cfg = next
    emulatorState.cfg = next
    emulatorState.gamesYMLInput = next.rpcs3.games_yml
    emulatorState.hdd0Input = next.rpcs3.hdd0_game
  }

  async function setUI<K extends keyof config.UI>(field: K, value: config.UI[K]) {
    if (!cfg) return
    await applyConfig(new config.Config({ ...cfg, ui: { ...cfg.ui, [field]: value } }))

    if (field === 'theme') {
      theme.set(value as any)
    }
  }

  async function setNetwork<K extends keyof config.Network>(field: K, value: config.Network[K]) {
    if (!cfg) return
    await applyConfig(
      new config.Config({ ...cfg, network: { ...cfg.network, [field]: value } })
    )
  }

  async function commitLibraryDir() {
    if (!cfg || libraryDirInput === cfg.storage.library_dir) return
    const newDir = libraryDirInput
    libraryDirError = await validatePath('library', newDir)
    if (libraryDirError) return
    await applyConfig(
      new config.Config({
        ...cfg,
        storage: { ...cfg.storage, library_dir: newDir },
      })
    )
    libraryDirInput = cfg.storage.library_dir
  }

  async function browseLibraryDir() {
    if (!cfg) return
    const picked = await PickDirectory('Select PSNWDL library folder', libraryDirInput || cfg.storage.library_dir)
    if (!picked) return
    libraryDirInput = picked
    await commitLibraryDir()
  }

  async function browseGamesYML() {
    if (!cfg) return
    const picked = await PickGamesYML(parentDir(gamesYMLInput))
    if (!picked) return
    gamesYMLInput = picked
    await commitGamesYML()
  }

  async function useDetectedGamesYML() {
    if (!detectedGamesYML) return
    gamesYMLInput = detectedGamesYML
    await commitGamesYML()
  }

  async function commitGamesYML() {
    if (!cfg || gamesYMLInput === cfg.rpcs3.games_yml) return
    gamesYMLError = await validatePath('games_yml', gamesYMLInput)
    if (gamesYMLError) return
    await applyConfig(
      new config.Config({
        ...cfg,
        rpcs3: { ...cfg.rpcs3, games_yml: gamesYMLInput },
      })
    )
  }

  async function browseHDD0Game() {
    if (!cfg) return
    const picked = await PickDirectory('Select dev_hdd0/game folder', hdd0Input)
    if (!picked) return
    hdd0Input = picked
    await commitHDD0Game()
  }

  async function commitHDD0Game() {
    if (!cfg || hdd0Input === cfg.rpcs3.hdd0_game) return
    hdd0Error = await validatePath('hdd0_game', hdd0Input)
    if (hdd0Error) return
    await applyConfig(
      new config.Config({
        ...cfg,
        rpcs3: { ...cfg.rpcs3, hdd0_game: hdd0Input },
      })
    )
  }

  function parentDir(path: string): string {
    if (!path) return ''
    const normalized = path.replaceAll('\\', '/')
    const idx = normalized.lastIndexOf('/')
    return idx > 0 ? path.slice(0, idx) : ''
  }

  async function validatePath(kind: 'library' | 'games_yml' | 'hdd0_game', path: string): Promise<string> {
    try {
      await ValidateSettingsPath(kind, path)
      return ''
    } catch (e) {
      return invalidPathMessage(kind)
    }
  }

  function invalidPathMessage(kind: 'library' | 'games_yml' | 'hdd0_game'): string {
    if (kind === 'games_yml') return 'Choose an existing file named games.yml'
    if (kind === 'hdd0_game') return 'Choose an existing dev_hdd0/game folder'
    return 'Choose a valid folder path'
  }

  async function commitMaxConcurrent() {
    if (!cfg || maxConcurrent === cfg.network.max_concurrent_downloads) return
    if (maxConcurrent < 1 || maxConcurrent > 100) {
      maxConcurrent = cfg.network.max_concurrent_downloads
      return
    }
    await setNetwork('max_concurrent_downloads', maxConcurrent)
  }

  async function commitRetryCount() {
    if (!cfg || retryCount === cfg.network.retry_count) return
    if (retryCount < 0 || retryCount > 20) {
      retryCount = cfg.network.retry_count
      return
    }
    await setNetwork('retry_count', retryCount)
  }

  const THEMES = [
    { value: 'system', label: 'System' },
    { value: 'dark', label: 'Dark' },
    { value: 'light', label: 'Light' },
  ]
</script>

<div class="page">
  <h1 class="mb-4 page-title">Settings</h1>

  {#if !cfg}
    <Loading />
  {:else}
    <div class="space-y-6">
      <section>
        <h2 class="mb-2 text-sm font-medium text-fg">Config</h2>
        <div class="panel p-4">
          <div class="grid grid-cols-[13rem_1fr] items-center gap-x-4 gap-y-2 text-sm">
            <div class="text-muted">Path</div>
            <div class="break-all font-mono text-fg">{configPath}</div>
          </div>
        </div>
      </section>

      <section>
        <h2 class="mb-2 text-sm font-medium text-fg">Interface</h2>
        <div class="panel p-4">
          <div class="grid grid-cols-[13rem_12rem_1fr] items-start gap-x-4 gap-y-4 text-sm">
            <label for="theme" class="pt-1 text-muted">Theme</label>
            <div>
              <select
                id="theme"
                value={cfg.ui.theme}
                onchange={(e) => setUI('theme', e.currentTarget.value)}
                class="input w-full px-2 py-1 text-sm"
              >
                {#each THEMES as t (t.value)}
                  <option value={t.value}>{t.label}</option>
                {/each}
              </select>
            </div>
            <span></span>

            <label for="default-mode" class="pt-1 text-muted">Default mode</label>
            <div>
              <select
                id="default-mode"
                value={cfg.ui.default_mode}
                onchange={(e) => setUI('default_mode', e.currentTarget.value)}
                class="input w-full px-2 py-1 text-sm"
              >
                {#each MODES as m (m.value)}
                  <option value={m.value}>{m.label}</option>
                {/each}
              </select>
            </div>
            <span></span>

            <label for="default-download" class="pt-1 text-muted">Default download</label>
            <div>
              <select
                id="default-download"
                value={cfg.ui.default_download}
                onchange={(e) => setUI('default_download', e.currentTarget.value)}
                class="input w-full px-2 py-1 text-sm"
              >
                <option value="firmware">Firmware</option>
                <option value="title">Title</option>
              </select>
            </div>
            <span></span>
          </div>
        </div>
      </section>

      <section>
        <h2 class="mb-2 text-sm font-medium text-fg">Network</h2>
        <div class="panel p-4">
          <div class="grid grid-cols-[13rem_12rem_1fr] items-start gap-x-4 gap-y-4 text-sm">
            <label for="max-concurrent" class="pt-1 text-muted">Concurrent downloads</label>
            <div>
              <input
                id="max-concurrent"
                type="number"
                min="1"
                max="100"
                bind:value={maxConcurrent}
                onchange={commitMaxConcurrent}
                class="input w-full px-2 py-1 text-sm"
              />
            </div>
            <span></span>

            <label for="retry-count" class="pt-1 text-muted">Retry count</label>
            <div>
              <input
                id="retry-count"
                type="number"
                min="0"
                max="20"
                bind:value={retryCount}
                onchange={commitRetryCount}
                class="input w-full px-2 py-1 text-sm"
              />
            </div>
            <span></span>
          </div>
        </div>
      </section>

      <section>
        <h2 class="mb-2 text-sm font-medium text-fg">Storage</h2>
        <div class="panel p-4">
          <div class="grid grid-cols-[13rem_12rem_1fr] items-start gap-x-4 gap-y-4 text-sm">
            <label for="library-dir" class="pt-1 text-muted">Library</label>
            <div class="col-span-2 min-w-0">
              <div class="flex gap-2">
                <input
                  id="library-dir"
                  bind:value={libraryDirInput}
                  oninput={() => (libraryDirError = '')}
                  onchange={commitLibraryDir}
                  class="input min-w-0 flex-1 px-2 py-1 font-mono text-sm"
                  class:invalid-input={libraryDirError}
                />
                <button onclick={browseLibraryDir} class="btn btn-secondary">Browse</button>
              </div>
            </div>
            <span></span>
            <span class="col-span-2 pt-1 text-xs {libraryDirError ? 'text-error-fg' : 'text-muted-soft'}">
              {libraryDirError || 'Download cache folder'}
            </span>
          </div>
        </div>
      </section>

      <section>
        <h2 class="mb-2 text-sm font-medium text-fg">Emulator</h2>
        <div class="space-y-3">
          <div>
            <div class="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-soft">RPCS3</div>
            <div class="panel p-4">
              <div class="grid grid-cols-[13rem_12rem_1fr] items-start gap-x-4 gap-y-4 text-sm">
                <label for="games-yml" class="pt-1 text-muted">games.yml</label>
                <div class="col-span-2 flex min-w-0 gap-2">
                  <input
                    id="games-yml"
                    bind:value={gamesYMLInput}
                    oninput={() => (gamesYMLError = '')}
                    onchange={commitGamesYML}
                    class="input min-w-0 flex-1 px-2 py-1 font-mono text-sm"
                    class:invalid-input={gamesYMLError}
                  />
                  {#if detectedGamesYML && gamesYMLInput !== detectedGamesYML}
                    <button onclick={useDetectedGamesYML} class="btn btn-secondary">Use detected</button>
                  {/if}
                  <button onclick={browseGamesYML} class="btn btn-secondary">Browse</button>
                </div>
                <span></span>
                <span class="col-span-2 pt-1 text-xs {gamesYMLError ? 'text-error-fg' : 'text-muted-soft'}">
                  {gamesYMLError || 'Game list file for emulator titles'}
                </span>

                <label for="hdd0-game" class="pt-1 text-muted">dev_hdd0/game</label>
                <div class="col-span-2 flex min-w-0 gap-2">
                  <input
                    id="hdd0-game"
                    bind:value={hdd0Input}
                    oninput={() => (hdd0Error = '')}
                    onchange={commitHDD0Game}
                    class="input min-w-0 flex-1 px-2 py-1 font-mono text-sm"
                    class:invalid-input={hdd0Error}
                  />
                  <button onclick={browseHDD0Game} class="btn btn-secondary">Browse</button>
                </div>
                <span></span>
                <span class="col-span-2 pt-1 text-xs {hdd0Error ? 'text-error-fg' : 'text-muted-soft'}">
                  {hdd0Error || 'Game install folder for Library PKGs'}
                </span>
              </div>
            </div>
          </div>
        </div>
      </section>

    </div>
  {/if}
</div>

<style>
  :global(.invalid-input) {
    border-color: var(--c-error);
    background: var(--c-error-bg);
  }
</style>
