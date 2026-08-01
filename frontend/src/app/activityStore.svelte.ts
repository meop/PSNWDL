import { derived, writable } from 'svelte/store'
import { ActivityLog, ClearActivityLog, ClearActivityLogScope } from '../../bindings/PSNWDL/app'
import { Events } from '@wailsio/runtime'
import type * as activity from '../../bindings/PSNWDL/internal/activity'

export type LogEntry = activity.Entry

export const activityEntries = writable<LogEntry[]>([])

let wired = false

export async function hydrateActivityLog(): Promise<void> {
  activityEntries.set(await ActivityLog())
}

export function wireActivityEvents(): void {
  if (wired) return
  wired = true
  Events.On('activity:log', ({ data: entry }) => {
    activityEntries.update((entries) => [...entries, entry].slice(-9000))
  })
}

export async function clearActivityLog(scope = 'all'): Promise<void> {
  if (scope === 'all') {
    await ClearActivityLog()
    activityEntries.set([])
    return
  }
  await ClearActivityLogScope(scope)
  activityEntries.update((entries) => entries.filter((entry) => entry.scope !== scope))
}

export const latestActivity = derived(activityEntries, ($entries) =>
  $entries.length ? $entries[$entries.length - 1] : null
)

export function formatLogLine(entry: LogEntry): string {
  const ts = new Date(entry.ts).toLocaleTimeString()
  const job = entry.job_id ? ` [${entry.job_id}]` : ''
  return `${ts} ${entry.level.toUpperCase()} ${entry.scope}${job} ${entry.message}`
}
