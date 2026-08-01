<script lang="ts">
  import { onMount, untrack } from 'svelte'
  import { MODES, type Mode } from './app/types'
  import { GetConfig } from '../bindings/PSNWDL/app'
  import { Events } from '@wailsio/runtime'
  import * as config from '../bindings/PSNWDL/internal/config'
  import { theme, type Theme } from './app/theme.svelte'
  import { activeJobsList, wireJobEvents, hydrateJobs } from './app/jobsStore.svelte'
  import { wireActivityEvents, hydrateActivityLog } from './app/activityStore.svelte'
  import { applyStartupWindowSize } from './app/windowSizing'
  import JobQueue from './components/JobQueue.svelte'
  import Workbench from './pages/Workbench.svelte'
  import Settings from './pages/Settings.svelte'

  interface Props {
    initialConfig?: config.Config | null
  }

  type Overlay = 'settings' | 'about' | null
  type DefaultDownload = 'firmware' | 'title'

  let { initialConfig = null }: Props = $props()
  const bootConfig = untrack(() => initialConfig)

  let appConfig = $state<config.Config | null>(bootConfig)
  let booted = $state(false)
  let bootError = $state<string | null>(null)
  let mode = $state<Mode>(normalizeMode(bootConfig?.ui?.default_mode))
  let defaultDownload = $state<DefaultDownload>(normalizeDefaultDownload(bootConfig?.ui?.default_download))
  let overlay = $state<Overlay>(null)
  let queueOpen = $state(false)
  const appVersion = '0.1.0'

  onMount(async () => {
    try {
      applyStartupWindowSize().catch(console.error)
      const cfg = appConfig ?? await GetConfig()
      appConfig = cfg
      applyConfigPreferences(cfg, true)
      wireJobEvents()
      wireActivityEvents()
      await hydrateJobs()
      await hydrateActivityLog()
      booted = true
    } catch (e) {
      bootError = e instanceof Error ? e.message : String(e)
    }
  })

  $effect(() => {
    const off = Events.On('config:updated', ({ data: cfg }) => {
      appConfig = cfg
      applyConfigPreferences(cfg, false)
    })
    return () => off()
  })

  function normalizeDefaultDownload(value: string | undefined): DefaultDownload {
    return value === 'title' ? 'title' : 'firmware'
  }

  function normalizeMode(value: string | undefined): Mode {
    return MODES.some((m) => m.value === value) ? (value as Mode) : 'ps3'
  }

  function applyConfigPreferences(cfg: config.Config, seedMode: boolean) {
    if (cfg?.ui) {
      const t = (cfg.ui.theme as Theme) ?? 'system'
      theme.set(t)
    }
    if (seedMode) mode = normalizeMode(cfg?.ui?.default_mode)
    defaultDownload = normalizeDefaultDownload(cfg?.ui?.default_download)
  }

  function toggleQueue() {
    overlay = null
    queueOpen = !queueOpen
  }

  function openOverlay(next: Exclude<Overlay, null>) {
    queueOpen = false
    overlay = next
  }
</script>

<div class="flex h-full flex-col">
  <header class="flex h-12 shrink-0 items-center gap-3 border-b border-border bg-surface px-4">
    <div class="flex items-center gap-2">
      <select bind:value={mode} disabled={!booted} class="input h-8 px-2 text-sm">
        {#each MODES as m (m.value)}
          <option value={m.value}>{m.label}</option>
        {/each}
      </select>
    </div>

    <div class="grow"></div>

    <button onclick={toggleQueue} disabled={!booted} aria-pressed={queueOpen} class="btn {queueOpen ? 'btn-primary' : 'btn-secondary'} min-h-8 min-w-8 px-2">
      Queue ({$activeJobsList.length})
    </button>
    <button onclick={() => openOverlay('settings')} disabled={!booted} aria-label="Settings" class="btn btn-secondary min-h-8 min-w-8 px-2">
      Settings
    </button>
    <button onclick={() => openOverlay('about')} disabled={!booted} aria-label="About" class="btn btn-secondary min-h-8 min-w-8 px-2">
      About
    </button>
  </header>

  <main class="min-h-0 grow overflow-hidden bg-bg p-3">
    {#if bootError}
      <p class="text-sm text-error-fg">Unable to load app config: {bootError}</p>
    {:else if !booted || !appConfig}
      <p class="text-sm text-muted-soft">Loading</p>
    {:else}
      <Workbench {mode} {defaultDownload} {appConfig} />
    {/if}
  </main>
</div>

{#if queueOpen}
  <div class="fixed inset-0 z-30">
    <button class="absolute inset-0 cursor-default border-0 bg-transparent p-0" aria-label="Close queue" onclick={() => (queueOpen = false)}></button>
    <section class="absolute right-3 top-14 w-[min(40rem,calc(100vw-1.5rem))] overflow-hidden rounded-lg border border-border bg-surface shadow-2xl" aria-label="Queue">
      <div class="flex h-11 items-center justify-between border-b border-border px-3">
        <div class="text-sm font-semibold text-fg-strong">Queue</div>
        <button onclick={() => (queueOpen = false)} class="btn btn-secondary">Close</button>
      </div>
      <JobQueue />
    </section>
  </div>
{/if}

{#if overlay}
  <div class="fixed inset-0 z-20 flex items-start justify-center p-5">
    <button class="absolute inset-0 bg-black/45" aria-label="Close overlay" onclick={() => (overlay = null)}></button>
    <div class="overlay-panel relative max-h-[calc(100vh-2.5rem)] min-h-0 overflow-hidden rounded-lg border border-border bg-surface shadow-2xl">
      <div class="flex h-12 items-center justify-between border-b border-border px-4">
        <div class="text-sm font-semibold text-fg-strong">
          {overlay === 'settings' ? 'Settings' : 'About'}
        </div>
        <button onclick={() => (overlay = null)} class="btn btn-secondary">Close</button>
      </div>
      <div class="max-h-[calc(100vh-5.5rem)] overflow-auto p-4">
        {#if overlay === 'settings'}
          <Settings />
        {:else}
          <div class="max-w-xl text-sm">
            <div class="grid grid-cols-[8rem_1fr] gap-x-4 gap-y-3">
              <div class="text-muted">Product</div>
              <div class="font-medium text-fg-strong">PSNWDL</div>
              <div class="text-muted">Description</div>
              <div class="text-fg">Modern PlayStation Network Download tool</div>
              <div class="text-muted">Version</div>
              <div class="font-mono text-fg">{appVersion}</div>
              <div class="text-muted">Credit</div>
              <div class="text-fg">Developed with help from GPT, Claude, and GLM</div>
            </div>
          </div>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .overlay-panel {
    width: min(68rem, calc(100vw - 2.5rem));
  }
</style>
