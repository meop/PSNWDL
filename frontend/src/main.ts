import { mount } from 'svelte'
import './style.css'
import App from './App.svelte'
import { GetConfig } from '../wailsjs/go/main/App'
import { theme, type Theme } from './app/theme.svelte'
import type { config } from '../wailsjs/go/models'

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
