import type * as downloads from '../../bindings/PSNWDL/internal/downloads'

export const downloadLibraryState = $state({
  titles: [] as downloads.Title[],
  selected: [] as string[],
  loading: false,
  updating: false,
  deleting: false,
  error: null as string | null,
  initialized: false,
})
