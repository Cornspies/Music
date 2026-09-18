"use client"

import { useEffect, useState } from "react"
import { toast } from "sonner"
import { Disc3 } from "lucide-react"
import { api } from "@/lib/api"
import { formatTotal } from "@/lib/format"
import type { Album } from "@/lib/types"
import { useUIStore } from "@/stores/ui"
import { CoverArt } from "@/components/music/CoverArt"
import { EmptyState } from "@/components/music/EmptyState"
import { Skeleton } from "@/components/ui/skeleton"

export function AlbumsView() {
  const navigate = useUIStore((s) => s.navigate)
  const [albums, setAlbums] = useState<Album[] | null>(null)

  useEffect(() => {
    let cancelled = false
    api<Album[]>("/api/albums")
      .then((a) => {
        if (!cancelled) setAlbums(a)
      })
      .catch((e: Error) => {
        if (!cancelled) {
          toast.error(e.message)
          setAlbums([])
        }
      })
    return () => {
      cancelled = true
    }
  }, [])

  return (
    <div className="p-4 md:p-6">
      <h1 className="text-2xl font-bold tracking-tight md:text-3xl">Albums</h1>

      <div className="mt-5">
        {albums === null ? (
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-5">
            {Array.from({ length: 10 }).map((_, i) => (
              <div key={i} className="p-2">
                <Skeleton className="aspect-square w-full rounded-xl" />
                <Skeleton className="mt-2 h-4 w-3/4 rounded-md" />
                <Skeleton className="mt-1.5 h-3 w-1/2 rounded-md" />
              </div>
            ))}
          </div>
        ) : albums.length === 0 ? (
          <EmptyState
            icon={Disc3}
            title="No albums yet"
          />
        ) : (
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-5">
            {albums.map((album) => {
              const key = JSON.stringify({ name: album.name, artist: album.artist })
              return (
                <button
                  key={key}
                  type="button"
                  onClick={() => navigate("album", key)}
                  className="group rounded-xl p-2 text-left transition-colors hover:bg-zinc-800/60"
                  aria-label={`Open album ${album.name} by ${album.artist}`}
                >
                  <div className="relative">
                    <CoverArt
                      trackId={album.hasCover ? album.coverTrackId : key}
                      title={album.name}
                      hasCover={album.hasCover}
                    />
                    <span className="absolute top-2 right-2 rounded-full bg-zinc-950/70 px-2 py-0.5 text-[10px] font-medium text-zinc-300 backdrop-blur">
                      {album.trackCount}
                    </span>
                  </div>
                  <p className="mt-2 truncate text-sm font-medium text-zinc-200">
                    {album.name || "Unknown album"}
                  </p>
                  <p className="truncate text-xs text-zinc-500">
                    {album.artist || "Unknown artist"} · {formatTotal(album.totalDuration)}
                  </p>
                </button>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}
