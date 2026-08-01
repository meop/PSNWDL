import { derived, writable } from 'svelte/store'
import { ActivityLog, ClearActivityLog } from '../../wailsjs/go/main/App'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import type { activity } from '../../wailsjs/go/models'

export type LogEntry = activity.Entry

export const activityEntries = writable<LogEntry[]>([])

let wired = false

export async function hydrateActivityLog(): Promise<void> {
  activityEntries.set(await ActivityLog())
}

export function wireActivityEvents(): void {
  if (wired) return
  wired = true
  EventsOn('activity:log', (entry: LogEntry) => {
    activityEntries.update((entries) => [...entries, entry].slice(-9000))
  })
}

export async function clearActivityLog(): Promise<void> {
  await ClearActivityLog()
  activityEntries.set([])
}

export const latestActivity = derived(activityEntries, ($entries) =>
  $entries.length ? $entries[$entries.length - 1] : null
)

export function formatLogLine(entry: LogEntry): string {
  const ts = new Date(entry.ts).toLocaleTimeString()
  const job = entry.job_id ? ` [${entry.job_id}]` : ''
  return `${ts} ${entry.level.toUpperCase()} ${entry.scope}${job} ${entry.message}`
}
