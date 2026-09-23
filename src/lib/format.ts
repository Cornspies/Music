export function formatDuration(sec: number): string {
  if (!Number.isFinite(sec) || sec < 0) return "0:00"
  const s = Math.floor(sec)
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const r = s % 60
  if (h > 0) return `${h}:${String(m).padStart(2, "0")}:${String(r).padStart(2, "0")}`
  return `${m}:${String(r).padStart(2, "0")}`
}

export function formatTotal(sec: number): string {
  if (!Number.isFinite(sec) || sec <= 0) return "0s"
  const s = Math.floor(sec)
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m ${s % 60}s`
  return `${s}s`
}

export function formatFileSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B"
  const units = ["B", "KB", "MB", "GB"]
  let i = 0
  let v = bytes
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v >= 10 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`
}

export function formatRelativeDate(iso: string): string {
  const then = new Date(iso).getTime()
  if (!Number.isFinite(then)) return ""
  const diffMs = Date.now() - then
  if (diffMs < 0) return "just now"
  const mins = Math.floor(diffMs / 60000)
  if (mins < 1) return "just now"
  if (mins < 60) return `${mins} minute${mins === 1 ? "" : "s"} ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours} hour${hours === 1 ? "" : "s"} ago`
  const days = Math.floor(hours / 24)
  if (days < 30) return `${days} day${days === 1 ? "" : "s"} ago`
  const months = Math.floor(days / 30)
  if (months < 12) return `${months} month${months === 1 ? "" : "s"} ago`
  const years = Math.floor(months / 12)
  return `${years} year${years === 1 ? "" : "s"} ago`
}

export function formatTimeUntil(iso: string): string {
  const at = new Date(iso).getTime()
  if (!Number.isFinite(at)) return ""
  const mins = Math.floor((at - Date.now()) / 60000)
  if (mins <= 0) return "expired"
  if (mins < 60) return `in ${mins} minute${mins === 1 ? "" : "s"}`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `in ${hours} hour${hours === 1 ? "" : "s"}`
  const days = Math.floor(hours / 24)
  return `in ${days} day${days === 1 ? "" : "s"}`
}

export function formatCountdown(totalSec: number): string {
  const s = Math.max(0, Math.ceil(totalSec))
  const m = Math.floor(s / 60)
  return `${m}:${String(s % 60).padStart(2, "0")}`
}


const GRADIENTS = [
  "from-emerald-700/70 via-teal-900/60 to-zinc-950",
  "from-teal-700/70 via-zinc-900 to-zinc-950",
  "from-lime-700/50 via-zinc-900 to-zinc-950",
  "from-amber-700/50 via-orange-900/40 to-zinc-950",
  "from-rose-800/50 via-zinc-900 to-zinc-950",
  "from-orange-700/50 via-rose-950/40 to-zinc-950",
]

export function hashGradient(seed: string): string {
  let h = 0
  const s = seed || "music"
  for (let i = 0; i < s.length; i++) {
    h = (h * 31 + s.charCodeAt(i)) | 0
  }
  return GRADIENTS[Math.abs(h) % GRADIENTS.length]
}

export function initialsOf(text: string): string {
  const parts = (text || "").trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return ""
  return parts
    .slice(0, 2)
    .map((p) => p[0]!)
    .join("")
    .toUpperCase()
}
