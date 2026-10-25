export function ago(iso: string | null | undefined): string {
  if (!iso) return '—'
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return '—'
  const s = Math.round((Date.now() - t) / 1000)
  const abs = Math.abs(s)
  const suffix = s >= 0 ? 'ago' : 'from now'
  if (abs < 60) return `${abs}s ${suffix}`
  if (abs < 3600) return `${Math.round(abs / 60)} min ${suffix}`
  if (abs < 86400) return `${Math.round(abs / 3600)} h ${suffix}`
  return `${Math.round(abs / 86400)} d ${suffix}`
}

export function dateTime(iso: string | null | undefined): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString()
}

export function shortSha(sha: string | null | undefined): string {
  return sha ? sha.slice(0, 7) : '—'
}

export function duration(s: number | undefined): string {
  if (s === undefined) return ''
  if (s < 60) return `${s}s`
  return `${Math.floor(s / 60)}m ${s % 60}s`
}

// A duration in ms, to the precision it is worth: 0.4 ms, 12 ms, 1.3 s.
export function millis(ms: number | undefined): string {
  if (ms === undefined) return '—'
  if (ms < 10) return `${ms.toFixed(1)} ms`
  if (ms < 1000) return `${Math.round(ms)} ms`
  return `${(ms / 1000).toFixed(1)} s`
}
