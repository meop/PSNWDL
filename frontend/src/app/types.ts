export type Mode = 'ps3' | 'ps4' | 'ps5' | 'psvita'

export const MODES: { value: Mode; label: string }[] = [
  { value: 'ps3', label: 'PS3' },
  { value: 'ps4', label: 'PS4' },
  { value: 'ps5', label: 'PS5' },
  { value: 'psvita', label: 'PSVita' },
]
