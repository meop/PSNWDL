import type * as psn from '../../bindings/PSNWDL/internal/psn'
import type { Mode } from './types'

export interface SearchState {
  titleID: string
  loading: boolean
  error: string | null
  result: psn.Title | null
}

function emptyState(): SearchState {
  return {
    titleID: '',
    loading: false,
    error: null,
    result: null,
  }
}

export const searchState = $state<Record<Mode, SearchState>>({
  ps3: emptyState(),
  ps4: emptyState(),
  ps5: emptyState(),
  psvita: emptyState(),
})

export function queuedKey(version: string, url: string): string {
  return `${version}-${url}`
}
