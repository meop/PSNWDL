import { ScreenGetAll, WindowCenter, WindowSetSize } from '../../wailsjs/runtime/runtime'

const candidates = [
  { width: 1920, height: 1080 },
  { width: 1600, height: 900 },
  { width: 1280, height: 720 },
]

export async function applyStartupWindowSize(): Promise<void> {
  const screen = await currentScreen()
  if (!screen) return

  const limitWidth = screen.width * 0.8
  const limitHeight = screen.height * 0.8
  const target =
    candidates.find((size) => size.width <= limitWidth && size.height <= limitHeight) ??
    candidates[candidates.length - 1]

  WindowSetSize(target.width, target.height)
  WindowCenter()
}

async function currentScreen(): Promise<{ width: number; height: number } | null> {
  const browserScreen = window.screen
  if (browserScreen?.availWidth && browserScreen?.availHeight) {
    return { width: browserScreen.availWidth, height: browserScreen.availHeight }
  }

  const screens = await ScreenGetAll()
  const screen = screens.find((s) => s.isCurrent) ?? screens.find((s) => s.isPrimary) ?? screens[0]
  return screen ? { width: screen.width, height: screen.height } : null
}
