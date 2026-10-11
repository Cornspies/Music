"use client"

import { Shuffle } from "lucide-react"
import { cn } from "@/lib/utils"
import type { ShuffleMode } from "@/stores/player"

export function shuffleLabel(mode: ShuffleMode): string {
  if (mode === "random") return "Shuffle on · click for shuffle without repeats"
  if (mode === "norepeat") return "Shuffle without repeats · click to turn off"
  return "Shuffle off"
}

export function ShuffleGlyph({ mode, className }: { mode: ShuffleMode; className?: string }) {
  return (
    <span className="relative inline-flex">
      <Shuffle className={className} />
      {mode === "norepeat" ? (
        <span
          aria-hidden="true"
          className={cn("absolute -top-1 -right-1.5 size-1.5 rounded-full bg-current ring-2 ring-zinc-950")}
        />
      ) : null}
    </span>
  )
}
