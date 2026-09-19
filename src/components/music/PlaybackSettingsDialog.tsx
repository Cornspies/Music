"use client"

import { SlidersHorizontal } from "lucide-react"
import { cn } from "@/lib/utils"
import { CROSSFADE_OPTIONS, usePlayerStore } from "@/stores/player"
import { useUIStore } from "@/stores/ui"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Switch } from "@/components/ui/switch"
import { Label } from "@/components/ui/label"

/** Crossfade + volume normalisation. Opened from the player bar. */
export function PlaybackSettingsDialog() {
  const open = useUIStore((s) => s.playbackSettingsOpen)
  const setOpen = useUIStore((s) => s.setPlaybackSettingsOpen)
  const crossfade = usePlayerStore((s) => s.crossfadeSeconds)
  const setCrossfade = usePlayerStore((s) => s.setCrossfade)
  const normalize = usePlayerStore((s) => s.normalizeVolume)
  const setNormalize = usePlayerStore((s) => s.setNormalizeVolume)

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent className="border-zinc-800 bg-zinc-950 text-zinc-100 sm:max-w-sm">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-base">
            <SlidersHorizontal className="size-4 text-emerald-400" aria-hidden="true" />
            Playback
          </DialogTitle>
          <DialogDescription className="text-xs text-zinc-500">
            How one track turns into the next.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-5 pt-1">
          <div>
            <p className="text-sm font-medium text-zinc-200">Crossfade</p>
            <p className="mb-2.5 text-xs text-zinc-500">
              Off means gapless: the next track is buffered ahead so it starts the
              instant this one ends.
            </p>
            <div role="radiogroup" aria-label="Crossfade length" className="flex gap-1.5">
              {CROSSFADE_OPTIONS.map((sec) => (
                <button
                  key={sec}
                  type="button"
                  role="radio"
                  aria-checked={crossfade === sec}
                  onClick={() => setCrossfade(sec)}
                  className={cn(
                    "flex-1 rounded-md border px-2 py-1.5 text-xs font-medium transition-colors",
                    crossfade === sec
                      ? "border-emerald-500/60 bg-emerald-500/15 text-emerald-300"
                      : "border-zinc-800 bg-zinc-900/60 text-zinc-400 hover:border-zinc-700 hover:text-zinc-200"
                  )}
                >
                  {sec === 0 ? "Off" : `${sec}s`}
                </button>
              ))}
            </div>
          </div>

          <div className="flex items-start justify-between gap-4">
            <div>
              <Label htmlFor="normalize" className="text-sm font-medium text-zinc-200">
                Normalize volume
              </Label>
              <p className="mt-0.5 text-xs text-zinc-500">
                Levels quiet and loud masters to the same loudness, using the
                server&apos;s analysis of each track.
              </p>
            </div>
            <Switch
              id="normalize"
              checked={normalize}
              onCheckedChange={setNormalize}
              className="mt-0.5 data-[state=checked]:bg-emerald-500"
            />
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
