import { writable, derived, get } from 'svelte/store'
import { ListJobs } from '../../wailsjs/go/main/App'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import type { jobs } from '../../wailsjs/go/models'

type Job = jobs.Job

/**
 * App-wide jobs store. Subscribed once at boot; both the Activity page and the
 * App footer read from it instead of each calling ListJobs/maintaining their
 * own copy. This is the single source for footer throughput/ETA.
 */

const _jobs = writable<Job[]>([])
let wired = false

export const jobsList = { subscribe: _jobs.subscribe }

function upsert(j: Job) {
  _jobs.update((list) => {
    const idx = list.findIndex((x) => x.id === j.id)
    if (idx === -1) return [...list, j]
    const next = list.slice()
    next[idx] = j
    return next
  })
}

/** Wire job events once (idempotent). Safe to call from multiple onMounts. */
export function wireJobEvents() {
  if (wired) return
  wired = true
  EventsOn('job:added', (j: Job) => upsert(j))
  EventsOn('job:state', (j: Job) => upsert(j))
  EventsOn('job:progress', (j: Job) => upsert(j))
}

/** Hydrate from backend (call once at boot). */
export async function hydrateJobs() {
  try {
    _jobs.set(await ListJobs())
  } catch {
    /* backend not ready */
  }
}

export interface AggregateStatus {
  active: number
  throughputBps: number
  etaSeconds: number
}

const ACTIVE: ReadonlySet<string> = new Set([
  'queued',
  'downloading',
  'paused',
  'verifying',
  'installing',
])

/**
 * Aggregate over active downloads for the status bar: count, summed MB/s,
 * and the slowest (max) ETA. Returns null when idle.
 */
export const aggregate = derived(_jobs, ($jobs): AggregateStatus | null => {
  let active = 0
  let throughput = 0
  let eta = 0
  for (const j of $jobs) {
    if (!ACTIVE.has(j.state)) continue
    active++
    throughput += j.throughput || 0
    if ((j.eta || 0) > eta) eta = j.eta || 0
  }
  if (active === 0) return null
  return { active, throughputBps: throughput, etaSeconds: eta }
})

export function formatThroughput(bytesPerSec: number): string {
  if (bytesPerSec < 1024) return `${bytesPerSec.toFixed(0)} B/s`
  const units = ['KB/s', 'MB/s', 'GB/s']
  let v = bytesPerSec / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(1)} ${units[i]}`
}

export function formatETA(seconds: number): string {
  if (seconds <= 0) return '—'
  if (seconds < 60) return `${Math.floor(seconds)}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${Math.floor(seconds % 60)}s`
  return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
}

// expose for Activity.svelte to push into the same list if needed
export const _internal = { upsert, get: () => get(_jobs) }
