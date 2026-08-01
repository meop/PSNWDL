<script lang="ts">
  import { CancelJob } from '../../bindings/PSNWDL/app'
  import type * as jobs from '../../bindings/PSNWDL/internal/jobs'
  import { jobsList } from '../app/jobsStore.svelte'

  const ACTIVE_JOB_STATES = new Set(['queued', 'downloading', 'paused', 'resuming', 'verifying'])
  let queueError = $state<string | null>(null)
  let activeJobs = $derived($jobsList.filter((job) => ACTIVE_JOB_STATES.has(String(job.state))))

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
</script>

<div class="max-h-[calc(100vh-8rem)] overflow-auto bg-surface">
  {#if activeJobs.length === 0}
    <div class="px-4 py-6 text-center text-sm text-muted">Queue is empty</div>
  {:else}
    <div class="divide-y divide-border">
      {#each activeJobs as job (job.id)}
        <div class="grid grid-cols-[minmax(0,1fr)_auto_auto_auto] items-center gap-3 px-3 py-2 text-xs">
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
    </div>
  {/if}

  {#if queueError}
    <div class="border-t border-error/40 bg-error-bg px-3 py-2 text-xs text-error-fg">{queueError}</div>
  {/if}
</div>
