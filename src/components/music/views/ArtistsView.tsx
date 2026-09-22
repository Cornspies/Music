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

      <div className="mt-4">
        {artists === null ? (
          <div className="grid grid-cols-3 gap-1 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6 xl:grid-cols-8">
            {Array.from({ length: 16 }).map((_, i) => (
              <div key={i} className="flex flex-col items-center gap-1.5 p-2">
                <Skeleton className="size-16 rounded-full md:size-20" />
                <Skeleton className="h-3.5 w-2/3 rounded-md" />
              </div>
            ))}
          </div>
        ) : artists.length === 0 ? (
          <EmptyState
            icon={Mic2}
            title="No artists yet"
          />
        ) : (
          <div className="grid grid-cols-3 gap-1 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6 xl:grid-cols-8">
            {artists.map((artist) => {
              const name = artist.name || "Unknown artist"
              return (
                <button
                  key={artist.name}
                  type="button"
                  onClick={() => navigate("artist", artist.name)}
                  className="group flex flex-col items-center gap-1.5 rounded-xl p-2 text-center transition-colors hover:bg-zinc-800/60"
                  aria-label={`Open artist ${name}`}
                >
                  <span
                    aria-hidden="true"
                    className={`flex size-16 items-center justify-center rounded-full bg-linear-to-br text-lg font-bold text-zinc-300/85 shadow-md transition-transform group-hover:scale-105 md:size-20 md:text-xl ${hashGradient(name)}`}
                  >
                    {initialsOf(name) || <Mic2 className="size-6 text-zinc-400/70" />}
                  </span>
                  <span className="max-w-full truncate text-xs font-medium text-zinc-200 sm:text-sm">
                    {name}
                  </span>
                  <span className="-mt-1 text-[11px] text-zinc-500">
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
