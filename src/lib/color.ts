function parseHex(hex: string): [number, number, number] | null {
  const m = /^#?([0-9a-f]{6})$/i.exec(hex.trim())
  if (!m) return null
  const n = parseInt(m[1], 16)
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255]
}

function toHex([r, g, b]: [number, number, number]): string {
  return "#" + [r, g, b].map((v) => Math.round(Math.max(0, Math.min(255, v))).toString(16).padStart(2, "0")).join("")
}

function luminance([r, g, b]: [number, number, number]): number {
  return (0.2126 * r + 0.7152 * g + 0.0722 * b) / 255
}

function lyricsSurface(coverColor: string | undefined): [number, number, number] {
  const rgb = parseHex(coverColor ?? "") ?? [63, 63, 70] // zinc-700
  const lum = luminance(rgb)
  if (lum > 0.32) {
    const k = 0.32 / lum
    return [rgb[0] * k, rgb[1] * k, rgb[2] * k]
  }
  if (lum < 0.12) {
    const t = (0.12 - lum) / (1 - lum)
    return [rgb[0] + (255 - rgb[0]) * t, rgb[1] + (255 - rgb[1]) * t, rgb[2] + (255 - rgb[2]) * t]
  }
  return rgb
}

export function lyricsGradient(coverColor: string | undefined): string {
  const top = lyricsSurface(coverColor)
  const bottom: [number, number, number] = [top[0] * 0.55, top[1] * 0.55, top[2] * 0.55]
  return `linear-gradient(180deg, ${toHex(top)} 0%, ${toHex(bottom)} 100%)`
}
