<script lang="ts">
  import { tick } from 'svelte'
  import { CancelJob } from '../../bindings/PSNWDL/app'
  import type * as jobs from '../../bindings/PSNWDL/internal/jobs'
  import { activityEntries, clearActivityLog, formatLogLine } from '../app/activityStore.svelte'
  import { jobsList } from '../app/jobsStore.svelte'

  interface Props {
    compact?: boolean
    embedded?: boolean
  }

  let { compact = false, embedded = false }: Props = $props()

  let logEntries = $derived($activityEntries)
  let selectedScope = $state<string>('all')
  let paused = $state(false)
  let queueError = $state<string | null>(null)
  let logContainer: HTMLElement
  const ACTIVE_JOB_STATES = new Set(['queued', 'downloading', 'paused', 'resuming', 'verifying'])
  let activeJobs = $derived($jobsList.filter((job) => ACTIVE_JOB_STATES.has(String(job.state))))

  async function clearLog() {
    await clearActivityLog(selectedScope)
  }

  function copyLog() {
    const text = filteredEntries.map(formatLogLine).join('\n')
    navigator.clipboard.writeText(text)
  }

  async function cancelJob(id: string) {
    queueError = null
    try {
      await CancelJob(id)
    } catch (e) {
      queueError = e instanceof Error ? e.message : String(e)
    }
  }

  function formatSize(bytes: number | undefined): string {
    if (!bytes || bytes <= 0) return '-'
    const units = ['B', 'KB', 'MB', 'GB', 'TB']
    let value = bytes
    let unit = 0
    while (value >= 1024 && unit < units.length - 1) {
      value /= 1024
      unit++
    }
    return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`
  }

  function formatProgress(job: jobs.Job): string {
    if (!job.update?.size) return formatSize(job.downloaded)
    return `${formatSize(job.downloaded)} / ${formatSize(job.update.size)}`
  }

  function canCancel(job: jobs.Job): boolean {
    return ['queued', 'downloading', 'paused', 'resuming'].includes(String(job.state))
  }

  const SCOPE_OPTIONS = ['all', 'psn', 'jobs', 'library', 'pkg']

  let filteredEntries = $derived(
    logEntries.filter((entry) => selectedScope === 'all' || entry.scope === selectedScope)
  )

  $effect(() => {
    const entries = filteredEntries
    const jobs = activeJobs
    if (paused || !logContainer) return

    void tick().then(() => {
      if (!paused && logContainer && entries === filteredEntries && jobs === activeJobs) {
        logContainer.scrollTop = logContainer.scrollHeight
      }
    })
  })

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
      <button onclick={() => (paused = !paused)} class="btn btn-secondary">
        {paused ? 'Resume scroll' : 'Pause scroll'}
      </button>
      <button onclick={clearLog} class="btn btn-secondary">Clear</button>
      <button onclick={copyLog} class="btn btn-secondary">Copy</button>
    </div>
  </div>

  {#if activeJobs.length > 0}
    <div class="max-h-32 overflow-auto border-b border-border bg-surface">
      {#each activeJobs as job (job.id)}
        <div class="grid grid-cols-[minmax(0,1fr)_auto_auto_auto] items-center gap-3 border-t border-border px-3 py-2 text-xs first:border-t-0">
          <div class="min-w-0 truncate text-fg" title={job.title_name || job.title_id}>
            {job.title_name || job.title_id} <span class="font-mono text-muted-soft">v{job.update?.version}</span>
          </div>
          <span class="text-muted">{job.state}</span>
          <span class="text-muted">{formatProgress(job)}</span>
          {#if canCancel(job)}
            <button onclick={() => cancelJob(job.id)} class="btn btn-secondary">Cancel</button>
          {:else}
            <span></span>
          {/if}
        </div>
      {/each}
      {#if queueError}
        <div class="border-t border-error/40 bg-error-bg px-3 py-2 text-xs text-error-fg">{queueError}</div>
      {/if}
    </div>
  {/if}

  <div class="{embedded ? 'border-b border-border px-3 py-2' : 'mb-3'} flex flex-wrap items-center gap-2">
    <span class="text-xs text-muted">Scope</span>
    {#each SCOPE_OPTIONS as scope}
      <button
        onclick={() => (selectedScope = scope)}
        class="btn {selectedScope === scope ? 'btn-primary' : 'btn-secondary'}"
      >
        {scope.charAt(0).toUpperCase() + scope.slice(1)}
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
