"use client"

import { Heart, Music2 } from "lucide-react"
import { hashGradient, formatTotal } from "@/lib/format"
import { useLibraryStore } from "@/stores/library"
import { useUIStore } from "@/stores/ui"
import { CollectionHeader } from "@/components/music/CollectionHeader"
import { EmptyState } from "@/components/music/EmptyState"
import { VirtualTrackList } from "@/components/music/VirtualTrackList"

export function FavoritesView() {
  const favorites = useLibraryStore((s) => s.favorites)
  const navigate = useUIStore((s) => s.navigate)

  const totalDuration = favorites.reduce((sum, t) => sum + t.duration, 0)

  return (
    <div>
      <CollectionHeader
        icon={Heart}
        iconGradient="from-emerald-700/70 via-zinc-900 to-zinc-950"
        eyebrow="Collection"
        title="Favorites"
        meta={`${favorites.length} track${favorites.length === 1 ? "" : "s"} · ${
          totalDuration > 0 ? formatTotal(totalDuration) : "nothing yet"
        }`}
        tracks={favorites}
        cover={
          <div
            aria-hidden="true"
            className={`flex aspect-square w-full items-center justify-center rounded-lg bg-linear-to-br shadow-2xl ${hashGradient("favorites")}`}
          >
            <Heart className="size-16 fill-current text-emerald-400/80 md:size-20" />
          </div>
        }
      />

      <div className="px-4 pt-5 pb-6 md:px-6">
        {favorites.length === 0 ? (
          <EmptyState
            icon={Heart}
            title="No favorites yet"
            compact
            actionLabel="Browse library"
            onAction={() => navigate("library")}
          />
        ) : (
          <VirtualTrackList tracks={favorites} />
        )}
      </div>
    </div>
  )
}
