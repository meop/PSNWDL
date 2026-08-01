import { writable, get } from 'svelte/store'
import {
  WindowSetDarkTheme,
  WindowSetLightTheme,
  WindowSetSystemDefaultTheme,
} from '../../wailsjs/runtime/runtime'

export type Theme = 'system' | 'dark' | 'light'

const prefersDark = () =>
  typeof window !== 'undefined' &&
  window.matchMedia('(prefers-color-scheme: dark)').matches

export function resolveTheme(theme: Theme): 'dark' | 'light' {
  if (theme === 'system') return prefersDark() ? 'dark' : 'light'
  return theme
}

/** Set `data-theme` on <html> to drive the CSS variable palette. */
export function applyTheme(theme: Theme) {
  document.documentElement.setAttribute('data-theme', resolveTheme(theme))
  try {
    if (theme === 'system') {
      WindowSetSystemDefaultTheme()
    } else if (theme === 'dark') {
      WindowSetDarkTheme()
    } else {
      WindowSetLightTheme()
    }
  } catch {
    /* Runtime theme APIs are available inside Wails, but not in browser preview. */
  }
}

/**
 * The user's theme preference. Resolved to dark/light via `applyTheme`.
 * 'system' tracks the OS preference live (re-applied on OS change).
 */
function createTheme() {
  const { subscribe, set } = writable<Theme>('system')

  let mediaMql: MediaQueryList | null = null
  let mediaHandler: (() => void) | null = null

  function trackSystem(current: Theme) {
    // Only one live listener at a time.
    if (mediaMql && mediaHandler) {
      mediaMql.removeEventListener('change', mediaHandler)
      mediaMql = null
      mediaHandler = null
    }
    if (current !== 'system') return

    mediaMql = window.matchMedia('(prefers-color-scheme: dark)')
    mediaHandler = () => applyTheme('system')
    mediaMql.addEventListener('change', mediaHandler)
  }

  return {
    subscribe,
    set(value: Theme) {
      set(value)
      applyTheme(value)
      trackSystem(value)
    },
    /** Read the current value without subscribing. */
    current(): Theme {
      return get({ subscribe } as any)
    },
  }
}

export const theme = createTheme()
