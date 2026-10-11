"use client"

import { useEffect, useState } from "react"
import { Disc3, ListPlus, Loader2 } from "lucide-react"
import { toast } from "sonner"
import { formatTotal } from "@/lib/format"
import { useLibraryStore } from "@/stores/library"
import { useUIStore } from "@/stores/ui"
import { CollectionHeader } from "@/components/music/CollectionHeader"
import { EmptyState } from "@/components/music/EmptyState"
import { TrackList } from "@/components/music/TrackList"
import { MixCover, useTracksById } from "@/components/music/HomeCards"
import { Button } from "@/components/ui/button"

export function MixView() {
  const mixId = useUIStore((s) => s.viewArg) ?? ""
  const home = useLibraryStore((s) => s.home)
  const [loadFailed, setLoadFailed] = useState(false)
  const [saving, setSaving] = useState(false)
  const mix = home?.mixes.find((m) => m.id === mixId) ?? null
  const tracks = useTracksById(mix?.trackIds)

  useEffect(() => {
    if (home) return
    useLibraryStore
      .getState()
      .fetchHome()
      .catch(() => setLoadFailed(true))
  }, [home])

  if (!home) {
    return loadFailed ? (
      <div className="p-6">
        <EmptyState icon={Disc3} title="Couldn't load this mix" compact />
      </div>
    ) : (
      <div className="flex items-center justify-center py-24 text-zinc-500">
        <Loader2 className="size-5 animate-spin" />
      </div>
    )
  }
  if (!mix) {
    return (
      <div className="p-6">
        <EmptyState icon={Disc3} title="This mix isn't around today" compact />
      </div>
    )
  }

  const saveAsPlaylist = async () => {
    setSaving(true)
    try {
      const library = useLibraryStore.getState()
      const playlist = await library.createPlaylist(mix.title)
      if (!playlist) return
      for (const t of tracks) {
        if (!(await library.addTrackToPlaylist(playlist.id, t.id))) return
      }
      toast.success(`Saved “${mix.title}” as a playlist`)
    } finally {
      setSaving(false)
    }
  }

  const duration = tracks.reduce((sum, t) => sum + t.duration, 0)

  return (
    <div>
      <CollectionHeader
        icon={Disc3}
        eyebrow={mix.kind === "artist" ? "Mix for you" : "Made for you"}
        title={mix.title}
        meta={`${mix.subtitle} · ${tracks.length} songs · ${formatTotal(duration)}`}
        tracks={tracks}
        cover={<MixCover mix={mix} />}
        actions={
          <Button
            variant="outline"
            onClick={saveAsPlaylist}
            disabled={saving || tracks.length === 0}
            className="h-10 rounded-full border-zinc-600/80 bg-zinc-900/60 px-5 text-zinc-200 hover:bg-zinc-800 hover:text-emerald-300"
          >
            {saving ? <Loader2 className="size-4 animate-spin" /> : <ListPlus className="size-4" />} Save as playlist
          </Button>
        }
      />
      <div className="px-4 pt-5 pb-6 md:px-6">
        <TrackList tracks={tracks} />
      </div>
    </div>
  )
}
