"use client"

import { useEffect, useState } from "react"
import { toast } from "sonner"
import { Loader2 } from "lucide-react"
import { cn } from "@/lib/utils"
import { fetchTrackCopies } from "@/lib/api"
import { describeCopy } from "@/lib/format"
import type { Track } from "@/lib/types"
import { useLibraryStore } from "@/stores/library"
import { CoverArt } from "@/components/music/CoverArt"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"

export function DeleteTrackDialog({
  track,
  open,
  onOpenChange,
}: {
  track: Track
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const [copies, setCopies] = useState<{ id: string; tracks: Track[] } | null>(null)
  const [into, setInto] = useState("")
  const [deleting, setDeleting] = useState(false)

  useEffect(() => {
    if (!open) return
    let alive = true
    fetchTrackCopies(track.id)
      .then((tracks) => {
        if (!alive) return
        setCopies({ id: track.id, tracks })
        setInto(tracks[0]?.id ?? "")
      })
      .catch(() => {
        if (alive) setCopies({ id: track.id, tracks: [] })
      })
    return () => {
      alive = false
    }
  }, [open, track.id])

  const found = copies?.id === track.id ? copies.tracks : null
  const keep = found?.find((t) => t.id === into)

  const confirm = async () => {
    setDeleting(true)
    try {
      if (await useLibraryStore.getState().deleteTrack(track.id, keep?.id)) {
        toast.success(`Deleted “${track.title}”`, keep ? { description: "The other copy took its place" } : undefined)
        onOpenChange(false)
      }
    } finally {
      setDeleting(false)
    }
  }

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete this track?</AlertDialogTitle>
          <AlertDialogDescription>
            {found && found.length > 0
              ? `“${track.title}” will be permanently removed. This cannot be undone.`
              : `“${track.title}” will be permanently removed from your library, playlists and favorites. This cannot be undone.`}
          </AlertDialogDescription>
        </AlertDialogHeader>

        {found && found.length > 0 ? (
          <div role="radiogroup" aria-label="Copy that takes its place" className="flex flex-col gap-1">
            <p className="mb-1 text-xs text-zinc-400">Put another copy in its place in playlists and favorites:</p>
            {found.map((c) => (
              <CopyOption key={c.id} selected={into === c.id} onSelect={() => setInto(c.id)}>
                <span className="w-9 shrink-0">
                  <CoverArt trackId={c.id} title={c.title} hasCover={c.hasCover} />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm text-zinc-100">
                    {c.title}
                    <span className="text-zinc-500"> · {c.artist || "Unknown artist"}</span>
                  </span>
                  <span className="block truncate text-[11px] text-zinc-500">{describeCopy(c)}</span>
                </span>
              </CopyOption>
            ))}
            <CopyOption selected={into === ""} onSelect={() => setInto("")}>
              <span className="text-sm text-zinc-300">None, just remove it</span>
            </CopyOption>
          </div>
        ) : null}

        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            disabled={deleting || found === null}
            className="bg-red-600 text-white hover:bg-red-500"
            onClick={(e) => {
              e.preventDefault()
              confirm()
            }}
          >
            {found === null ? <Loader2 className="size-4 animate-spin" /> : null} Delete
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

function CopyOption({
  selected,
  onSelect,
  children,
}: {
  selected: boolean
  onSelect: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      className={cn(
        "flex w-full items-center gap-3 rounded-lg border px-3 py-2 text-left transition-colors",
        selected ? "border-emerald-600/70 bg-emerald-600/10" : "border-zinc-800 hover:bg-zinc-800/60"
      )}
    >
      <span
        aria-hidden="true"
        className={cn(
          "flex size-4 shrink-0 items-center justify-center rounded-full border",
          selected ? "border-emerald-400" : "border-zinc-600"
        )}
      >
        {selected ? <span className="size-2 rounded-full bg-emerald-400" /> : null}
      </span>
      {children}
    </button>
  )
}
