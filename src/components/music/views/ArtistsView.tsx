"use client"

import { useEffect, useState } from "react"
import { toast } from "sonner"
import { Mic2 } from "lucide-react"
import { api } from "@/lib/api"
import { formatTotal } from "@/lib/format"
import { hashGradient, initialsOf } from "@/lib/format"
import type { Artist } from "@/lib/types"
import { useUIStore } from "@/stores/ui"
import { EmptyState } from "@/components/music/EmptyState"
import { Skeleton } from "@/components/ui/skeleton"

export function ArtistsView() {
  const navigate = useUIStore((s) => s.navigate)
  const [artists, setArtists] = useState<Artist[] | null>(null)

  useEffect(() => {
    let cancelled = false
    api<Artist[]>("/api/artists")
      .then((a) => {
        if (!cancelled) setArtists(a)
      })
      .catch((e: Error) => {
        if (!cancelled) {
          toast.error(e.message)
          setArtists([])
        }
      })
    return () => {
      cancelled = true
    }
  }, [])

  return (
    <div className="p-4 md:p-6">
      <h1 className="text-2xl font-bold tracking-tight md:text-3xl">Artists</h1>

      <div className="mt-5">
        {artists === null ? (
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-5">
            {Array.from({ length: 10 }).map((_, i) => (
              <div key={i} className="flex flex-col items-center gap-2 p-2">
                <Skeleton className="size-full aspect-square rounded-full" />
                <Skeleton className="h-4 w-2/3 rounded-md" />
              </div>
            ))}
          </div>
        ) : artists.length === 0 ? (
          <EmptyState
            icon={Mic2}
            title="No artists yet"
          />
        ) : (
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-5">
            {artists.map((artist) => {
              const name = artist.name || "Unknown artist"
              return (
                <button
                  key={artist.name}
                  type="button"
                  onClick={() => navigate("artist", artist.name)}
                  className="group flex flex-col items-center gap-2 rounded-xl p-3 text-center transition-colors hover:bg-zinc-800/60"
                  aria-label={`Open artist ${name}`}
                >
                  <span
                    aria-hidden="true"
                    className={`flex size-24 items-center justify-center rounded-full bg-linear-to-br text-2xl font-bold text-zinc-300/85 shadow-lg transition-transform group-hover:scale-105 md:size-28 ${hashGradient(name)}`}
                  >
                    {initialsOf(name) || <Mic2 className="size-8 text-zinc-400/70" />}
                  </span>
                  <span className="mt-1 max-w-full truncate text-sm font-medium text-zinc-200">
                    {name}
                  </span>
                  <span className="text-xs text-zinc-500">
                    {artist.trackCount} track{artist.trackCount === 1 ? "" : "s"} ·{" "}
                    {formatTotal(artist.totalDuration)}
                  </span>
                </button>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}
