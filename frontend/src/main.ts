import { mount } from 'svelte'
import './style.css'
import App from './App.svelte'
import { GetConfig } from '../bindings/PSNWDL/app'
import { theme, type Theme } from './app/theme.svelte'
import type * as config from '../bindings/PSNWDL/internal/config'

const target = document.getElementById('app')
if (!target) throw new Error('#app not found')

async function loadInitialConfig(): Promise<config.Config | null> {
  try {
    return await GetConfig()
  } catch {
    return null
  }
}

const initialConfig = await loadInitialConfig()
const initialTheme = (initialConfig?.ui?.theme as Theme | undefined) ?? 'system'
theme.set(initialTheme)

export default mount(App, { target, props: { initialConfig } })
