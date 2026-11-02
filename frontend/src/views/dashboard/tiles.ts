// 顺序与首页卡片一致(从上到下、行内从左到右):
// 顶部概览(系统资源 / 服务器 / 关键指标)→ 图表(流量 / 协议占比 / 网络吞吐)→ 动态
export const TILES = [
  { id: 'resources', k: 'systemResources' },
  { id: 'server', k: 'server' },
  { id: 'keymetrics', k: 'keymetrics' },
  { id: 'traffic', k: 'traffic' },
  { id: 'protocol', k: 'protocolMix' },
  { id: 'network', k: 'networkThroughput' },
  { id: 'nodes', k: 'nodesTile' },
  { id: 'activity', k: 'activity' },
  { id: 'targetStats', k: 'targetStats' },
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
