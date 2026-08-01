import { config } from '../../wailsjs/go/models'
import type { library } from '../../wailsjs/go/models'

export const libraryState = $state({
  cfg: null as config.Config | null,
  rows: [] as library.Row[],
  loadError: null as string | null,
  loading: false,
  detectedPath: '',
  gamesYMLInput: '',
  hdd0Input: '',
  batchInstalling: false,
  batchError: null as string | null,
  initialized: false,
})
