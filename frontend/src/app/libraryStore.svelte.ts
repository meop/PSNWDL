import * as config from '../../bindings/PSNWDL/internal/config'
import type * as library from '../../bindings/PSNWDL/internal/library'

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
