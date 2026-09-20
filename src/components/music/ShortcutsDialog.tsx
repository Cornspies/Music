"use client"

import { Keyboard } from "lucide-react"
import { useUIStore } from "@/stores/ui"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

const SHORTCUTS: { keys: string; action: string }[] = [
  { keys: "Space", action: "Play / pause" },
  { keys: "← →", action: "Seek 10 seconds back / forward" },
  { keys: "↑ ↓", action: "Volume up / down" },
  { keys: "M", action: "Mute / unmute" },
  { keys: "N", action: "Next track" },
  { keys: "P", action: "Previous track" },
  { keys: "S", action: "Toggle shuffle" },
  { keys: "R", action: "Cycle repeat (off → all → one)" },
  { keys: "F", action: "Favorite the current track" },
  { keys: "Q", action: "Open / close the queue" },
  { keys: "O", action: "Expand now playing" },
  { keys: "/", action: "Jump to search" },
  { keys: "?", action: "Show this help" },
  { keys: "Esc", action: "Close overlays" },
]

/** Keyboard shortcuts reference dialog ("?" or the sidebar button opens it). */
export function ShortcutsDialog() {
  const open = useUIStore((s) => s.shortcutsOpen)
  const setOpen = useUIStore((s) => s.setShortcutsOpen)

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent
        aria-describedby={undefined}
        // Reference sheet only — no input, so the shortcuts it documents keep
        // working while you read it.
        data-shortcuts-allowed=""
        className="max-w-md gap-0 border-zinc-800 bg-zinc-950 p-0 text-zinc-100 sm:max-w-md"
      >
        <DialogHeader className="border-b border-zinc-800/70 px-5 py-4">
          <DialogTitle className="flex items-center gap-2 text-base">
            <Keyboard className="size-4 text-emerald-400" aria-hidden="true" />
            Keyboard shortcuts
          </DialogTitle>
        </DialogHeader>
        <div className="max-h-[60vh] overflow-y-auto p-2">
          {SHORTCUTS.map((s) => (
            <div
              key={s.action}
              className="flex items-center justify-between gap-4 rounded-lg px-3 py-2 hover:bg-zinc-800/50"
            >
              <span className="text-sm text-zinc-300">{s.action}</span>
              <kbd className="shrink-0 rounded-md border border-zinc-700 bg-zinc-900 px-2 py-0.5 font-mono text-[11px] font-medium text-zinc-300 shadow-sm">
                {s.keys}
              </kbd>
            </div>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  )
}
