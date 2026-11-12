export const COVER_CACHE = "music-covers-v1"

const versions = new Map<string, string>()

export function noteCoverVersions(tracks: Iterable<{ id: string; updatedAt?: string }>): void {
  for (const t of tracks) {
    if (t.updatedAt) versions.set(t.id, t.updatedAt)
  }
}

export function coverVersion(id: string): string | undefined {
  return versions.get(id)
}

export function dropCoverCache(): void {
  versions.clear()
  try {
    if (typeof caches !== "undefined") caches.delete(COVER_CACHE).catch(() => {})
  } catch {}
}
