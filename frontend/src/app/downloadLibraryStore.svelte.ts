import type { downloads } from '../../wailsjs/go/models'

export const downloadLibraryState = $state({
  titles: [] as downloads.Title[],
  selected: [] as string[],
  loading: false,
  updating: false,
  deleting: false,
  error: null as string | null,
  initialized: false,
})
