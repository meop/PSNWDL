import { writable, derived, get } from 'svelte/store'
import type * as psn from '../../bindings/PSNWDL/internal/psn'
import { ListFirmware } from '../../bindings/PSNWDL/app'
import type { Mode } from './types'

// Per-mode firmware cache with a freshness timestamp, so switching
// Firmware -> Search -> Firmware does not re-hit the network every time.
// Refresh is explicit; entries expire after `ttlMs`.
//
// Kept in a store so it survives page unmount/remount (Library/Activity/etc.
// don't hold a reference). Session-scoped — cleared on restart like everything
// else (no disk).

const ttlMs = 2 * 60 * 1000 // 2 minutes

interface Cached {
  list: psn.FirmwareList
  fetchedAt: number // Date.now()
}

/** The raw cache store — subscribe to read per-mode entries. */
export const cache = writable<Partial<Record<Mode, Cached>>>({})

export const fetching = writable<Mode | null>(null)

/** Return cached list if fresh enough, else null. */
function fresh(c: Cached | undefined): c is Cached {
  return !!c && Date.now() - c.fetchedAt < ttlMs
}

/**
 * Ensure a fresh firmware list for `mode` is available. Fetches only when
 * there is no fresh cache (or `force`). Safe to call repeatedly.
 */
export async function ensureFirmware(mode: Mode, force = false): Promise<void> {
  if (!force && fresh(get(cache)[mode])) return
  fetching.set(mode)
  try {
    const list = await ListFirmware(mode)
    if (!list) throw new Error(`No firmware list returned for ${mode}`)
    cache.update((c) => ({ ...c, [mode]: { list, fetchedAt: Date.now() } }))
  } finally {
    fetching.set(null)
  }
}

/**
 * Format the age of a cached entry. Takes the snapshot (not the mode) so the
 * caller reads it from a `$derived` that tracks both the cache and mode.
 */
export function ageOf(entry: Cached | null | undefined): string | null {
  if (!entry) return null
  const secs = Math.max(0, Math.round((Date.now() - entry.fetchedAt) / 1000))
  if (secs < 60) return `${secs}s ago`
  return `${Math.round(secs / 60)}m ago`
}

export type { Cached }
