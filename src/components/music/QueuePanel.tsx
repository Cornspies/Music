"use client"

import { ListMusic, Play, X } from "lucide-react"
import { cn } from "@/lib/utils"
import { formatDuration } from "@/lib/format"
import { usePlayerStore } from "@/stores/player"
import { useUIStore } from "@/stores/ui"
import { CoverArt } from "@/components/music/CoverArt"
import { EqualizerIcon } from "@/components/music/EqualizerIcon"
import { Button } from "@/components/ui/button"
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/components/ui/sheet"

export function QueuePanel() {
  const queueOpen = useUIStore((s) => s.queueOpen)
  const setQueueOpen = useUIStore((s) => s.setQueueOpen)

  const currentTrack = usePlayerStore((s) => s.currentTrack)
  const queue = usePlayerStore((s) => s.queue)
  const queueIndex = usePlayerStore((s) => s.queueIndex)
  const isPlaying = usePlayerStore((s) => s.isPlaying)

  const upcoming = queue.slice(queueIndex + 1)

  return (
    <Sheet open={queueOpen} onOpenChange={setQueueOpen}>
      <SheetContent
        side="right"
        // Passive overlay: it shows the queue but takes no text input, so the
        // global shortcuts stay live while it's open (see use-keyboard-shortcuts).
        data-shortcuts-allowed=""
        className="flex w-full flex-col gap-0 border-zinc-800/70 bg-zinc-950 p-0 sm:max-w-md"
      >
        <SheetHeader className="border-b border-zinc-800/70 px-4 py-4">
          <SheetTitle className="flex items-center gap-2 text-base">
            <ListMusic className="size-4 text-emerald-400" aria-hidden="true" /> Queue
          </SheetTitle>
          <SheetDescription className="text-xs text-zinc-500">
            {queue.length > 0
              ? `${queue.length} track${queue.length === 1 ? "" : "s"} in the queue`
              : "Nothing queued yet"}
          </SheetDescription>
        </SheetHeader>

        <div className="flex-1 overflow-y-auto px-2 py-3">
          {currentTrack ? (
            <section aria-label="Now playing">
              <h3 className="px-2 pb-2 text-[11px] font-semibold uppercase tracking-widest text-zinc-500">
                Now playing
              </h3>
              <div className="flex items-center gap-3 rounded-lg bg-zinc-900/80 px-2 py-2">
                <div className="w-10 shrink-0">
                  <CoverArt
                    trackId={currentTrack.id}
                    title={currentTrack.title}
                    hasCover={currentTrack.hasCover}
                  />
                </div>
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium text-emerald-400">
                    {currentTrack.title}
                  </div>
                  <div className="truncate text-xs text-zinc-500">
                    {currentTrack.artist || "Unknown artist"}
                  </div>
                </div>
                <EqualizerIcon playing={isPlaying} className="mr-2" />
              </div>
            </section>
          ) : (
            <p className="px-4 py-6 text-center text-sm text-zinc-600">
              Nothing playing — pick a track from your library.
            </p>
          )}

          {upcoming.length > 0 && (
            <section aria-label="Next up" className="mt-4">
              <div className="flex items-center justify-between px-2 pb-2">
                <h3 className="text-[11px] font-semibold uppercase tracking-widest text-zinc-500">
                  Next up
                </h3>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => usePlayerStore.getState().clearQueue()}
                  className="h-7 px-2 text-xs text-zinc-500 hover:text-zinc-200"
                >
                  Clear
                </Button>
              </div>
              {upcoming.map((track, i) => {
                const realIndex = queueIndex + 1 + i
                return (
                  <div
                    key={`${track.id}-${realIndex}`}
                    role="button"
                    tabIndex={0}
                    onClick={() => usePlayerStore.getState().jumpTo(realIndex)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") usePlayerStore.getState().jumpTo(realIndex)
                    }}
                    className="group flex cursor-pointer items-center gap-3 rounded-lg px-2 py-1.5 hover:bg-zinc-800/60"
                  >
                    <span className="w-4 shrink-0 text-center text-[11px] tabular-nums text-zinc-600 group-hover:hidden" />
                    <Play className="hidden size-3 shrink-0 fill-current text-zinc-300 group-hover:block" />
                    <div className="w-9 shrink-0">
                      <CoverArt trackId={track.id} title={track.title} hasCover={track.hasCover} />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-sm text-zinc-200">{track.title}</div>
                      <div className="truncate text-xs text-zinc-500">
                        {track.artist || "Unknown artist"}
                      </div>
                    </div>
                    <span className="shrink-0 text-[11px] tabular-nums text-zinc-600">
                      {formatDuration(track.duration)}
                    </span>
                    <button
                      type="button"
                      aria-label={`Remove ${track.title} from queue`}
                      onClick={(e) => {
                        e.stopPropagation()
                        usePlayerStore.getState().removeFromQueue(realIndex)
                      }}
                      className={cn(
                        "flex size-7 shrink-0 items-center justify-center rounded-md text-zinc-500",
                        "opacity-0 group-hover:opacity-100 hover:text-red-400"
                      )}
                    >
                      <X className="size-4" />
                    </button>
                  </div>
                )
              })}
            </section>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}
