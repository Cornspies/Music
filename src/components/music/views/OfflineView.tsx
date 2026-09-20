"use client"

import { useState } from "react"
import { CloudDownload, HardDrive, Trash2 } from "lucide-react"
import { formatFileSize, formatTotal, hashGradient } from "@/lib/format"
import { useOfflineStore } from "@/stores/offline"
import { useUIStore } from "@/stores/ui"
import { CollectionHeader } from "@/components/music/CollectionHeader"
import { EmptyState } from "@/components/music/EmptyState"
import { TrackList } from "@/components/music/TrackList"
import { Button } from "@/components/ui/button"
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

/** Tracks stored on this device for playback without a connection. */
export function OfflineView() {
  const supported = useOfflineStore((s) => s.supported)
  const tracks = useOfflineStore((s) => s.tracks)
  const bytesUsed = useOfflineStore((s) => s.bytesUsed)
  const jobs = useOfflineStore((s) => s.jobs)
  const removeAll = useOfflineStore((s) => s.removeAll)
  const navigate = useUIStore((s) => s.navigate)
  const [confirmOpen, setConfirmOpen] = useState(false)

  const inFlight = Object.keys(jobs).length
  const totalDuration = tracks.reduce((sum, t) => sum + t.duration, 0)

  return (
    <div>
      <CollectionHeader
        icon={CloudDownload}
        iconGradient="from-sky-700/70 via-zinc-900 to-zinc-950"
        eyebrow="On this device"
        title="Downloads"
        meta={
          <>
            {tracks.length} track{tracks.length === 1 ? "" : "s"} ·{" "}
            {totalDuration > 0 ? formatTotal(totalDuration) : "nothing yet"} · {formatFileSize(bytesUsed)}
            {inFlight > 0 ? ` · ${inFlight} downloading…` : ""}
          </>
        }
        tracks={tracks}
        cover={
          <div
            aria-hidden="true"
            className={`flex aspect-square w-full items-center justify-center rounded-lg bg-linear-to-br shadow-2xl ${hashGradient("offline")}`}
          >
            <HardDrive className="size-16 text-sky-300/80 md:size-20" />
          </div>
        }
        actions={
          tracks.length > 0 ? (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setConfirmOpen(true)}
              className="text-zinc-400 hover:text-red-300"
            >
              <Trash2 className="size-4" /> Remove all
            </Button>
          ) : null
        }
      />

      <div className="px-4 pt-5 pb-6 md:px-6">
        {!supported ? (
          <EmptyState
            icon={CloudDownload}
            title="Offline playback isn't supported in this browser"
            compact
          />
        ) : tracks.length === 0 ? (
          <EmptyState
            icon={CloudDownload}
            title="Nothing downloaded yet"
            compact
            actionLabel="Browse library"
            onAction={() => navigate("library")}
          />
        ) : (
          <TrackList tracks={tracks} />
        )}
      </div>

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent className="border-zinc-800 bg-zinc-950 text-zinc-100">
          <AlertDialogHeader>
            <AlertDialogTitle>Remove all downloads?</AlertDialogTitle>
            <AlertDialogDescription className="text-zinc-400">
              This frees {formatFileSize(bytesUsed)} on this device. Nothing on the server changes.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel className="border-zinc-800 bg-transparent text-zinc-300 hover:bg-zinc-800">
              Cancel
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={() => removeAll()}
              className="bg-red-600 text-white hover:bg-red-500"
            >
              Remove all
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
