import { writable } from 'svelte/store'
import { ListJobs } from '../../bindings/PSNWDL/app'
import { Events } from '@wailsio/runtime'
import type * as jobs from '../../bindings/PSNWDL/internal/jobs'

type Job = jobs.Job

/**
 * App-wide jobs store. Activity uses it for the active shared-queue view, and
 * Workbench uses it to disable duplicate downloads and find installable jobs.
 * Terminal jobs remain available for install decisions but Activity filters
 * them out of its queue view.
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
  Events.On('job:added', ({ data }) => upsert(data))
  Events.On('job:state', ({ data }) => upsert(data))
  Events.On('job:progress', ({ data }) => upsert(data))
}

/** Hydrate from backend (call once at boot). */
export async function hydrateJobs() {
  try {
    _jobs.set(await ListJobs())
  } catch {
    /* backend not ready */
  }
}
