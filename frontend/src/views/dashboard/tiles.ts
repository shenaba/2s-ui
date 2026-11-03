// Matches card order on the dashboard (top to bottom, left to right within rows):
// Top overview (resources / server / key metrics) -> charts (traffic / protocol mix / network throughput) -> target stats -> activity
export const TILES = [
  { id: 'resources', k: 'systemResources' },
  { id: 'server', k: 'server' },
  { id: 'keymetrics', k: 'keymetrics' },
  { id: 'traffic', k: 'traffic' },
  { id: 'protocol', k: 'protocolMix' },
  { id: 'network', k: 'networkThroughput' },
  { id: 'nodes', k: 'nodesTile' },
  { id: 'targetStats', k: 'targetStats' },
  { id: 'activity', k: 'activity' },
]

export const TILES_KEY = '2sui-tiles'

export function defaultTiles(): Record<string, boolean> {
  return Object.fromEntries(TILES.map((t) => [t.id, true]))
}

export function loadTiles(): Record<string, boolean> {
  try {
    const raw = localStorage.getItem(TILES_KEY)
    if (raw) return { ...defaultTiles(), ...JSON.parse(raw) }
  } catch { /* corrupted value -> defaults */ }
  return defaultTiles()
}

export function saveTiles(map: Record<string, boolean>) {
  localStorage.setItem(TILES_KEY, JSON.stringify(map))
}
