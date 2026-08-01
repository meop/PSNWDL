<script lang="ts">
  import { activityEntries, clearActivityLog, formatLogLine } from '../app/activityStore.svelte'

  interface Props {
    compact?: boolean
    embedded?: boolean
  }

  let { compact = false, embedded = false }: Props = $props()

  let logEntries = $derived($activityEntries)
  let selectedScope = $state<string>('all')
  let paused = $state(false)
  let logContainer: HTMLElement

  $effect(() => {
    if (!paused && logContainer) {
      logContainer.scrollTop = logContainer.scrollHeight
    }
  })

  async function clearLog() {
    await clearActivityLog()
  }

  function copyLog() {
    const text = logEntries.map(formatLogLine).join('\n')
    navigator.clipboard.writeText(text)
  }

  const SCOPE_OPTIONS = ['all', 'psn', 'jobs', 'library', 'pkg']

  let filteredEntries = $derived(
    logEntries.filter((entry) => selectedScope === 'all' || entry.scope === selectedScope)
  )

  function formatTimestamp(ts: string): string {
    return new Date(ts).toLocaleTimeString()
  }

  const LEVEL_COLORS: Record<string, string> = {
    info: 'text-fg',
    warn: 'text-warn-fg',
    error: 'text-error-fg',
  }
</script>

<div class:page={!compact && !embedded} class:h-full={embedded} class:min-h-0={embedded} class:flex={embedded} class:flex-col={embedded}>
  <div class="{embedded ? 'mb-0 border-b border-border px-3 py-2' : 'page-header'} flex items-center justify-between gap-3">
    <div>
      <h1 class="{embedded ? 'text-sm font-semibold text-fg-strong' : 'page-title'}">Activity</h1>
      <p class="mt-1 text-xs text-muted">PSN, jobs, library, and package operations</p>
    </div>
    <div class="flex items-center gap-2">
      <button onclick={clearLog} class="btn btn-secondary">Clear</button>
      <button onclick={copyLog} class="btn btn-secondary">Copy</button>
      <button onclick={() => (paused = !paused)} class="btn btn-secondary">
        {paused ? 'Unpause scroll' : 'Pause scroll'}
      </button>
    </div>
  </div>

  <div class="{embedded ? 'border-b border-border px-3 py-2' : 'mb-3'} flex flex-wrap items-center gap-2">
    <span class="text-xs text-muted">Scope</span>
    {#each SCOPE_OPTIONS as scope}
      <button
        onclick={() => (selectedScope = scope)}
        class="btn {selectedScope === scope ? 'btn-primary' : 'btn-secondary'}"
      >
        {scope}
      </button>
    {/each}
  </div>

  <div
    bind:this={logContainer}
    class="{embedded ? 'min-h-0 flex-1 rounded-none border-0' : compact ? 'h-[calc(100vh-14rem)]' : 'h-[calc(100vh-12rem)]'} overflow-y-auto rounded border border-border bg-inset p-3 font-mono text-xs"
  >
    {#if filteredEntries.length === 0}
      <p class="text-muted-faint">No activity yet</p>
    {:else}
      {#each filteredEntries as entry, i (`${entry.ts}-${i}`)}
        <div class="mb-1 {LEVEL_COLORS[entry.level] ?? ''}">
          <span class="opacity-60">{formatTimestamp(entry.ts)}</span>
          <span class="opacity-60"> {entry.scope}</span>
          {#if entry.job_id}
            <span class="opacity-60"> [{entry.job_id}]</span>
          {/if}
          <span class="ml-1">{entry.message}</span>
        </div>
      {/each}
    {/if}
  </div>
</div>
