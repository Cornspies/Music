"use client"

import { useEffect, useState } from "react"
import { Moon, MoonStar } from "lucide-react"
import { cn } from "@/lib/utils"
import { formatCountdown } from "@/lib/format"
import { usePlayerStore } from "@/stores/player"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

const OPTIONS: { minutes: number | null; label: string }[] = [
  { minutes: 15, label: "15 minutes" },
  { minutes: 30, label: "30 minutes" },
  { minutes: 60, label: "1 hour" },
  { minutes: 0, label: "End of current track" },
]

function useSleepCountdown(endsAt: number | null): number | null {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (endsAt === null) return
    // first update on the next frame (async → no cascading render),
    // then keep ticking once a second until the timer fires.
    const raf = requestAnimationFrame(() => setNow(Date.now()))
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => {
      cancelAnimationFrame(raf)
      clearInterval(id)
    }
  }, [endsAt])
  return endsAt === null ? null : Math.max(0, (endsAt - now) / 1000)
}

/**
 * Sleep timer dropdown — shared by the PlayerBar (desktop) and the
 * Now Playing overlay. 0 = "end of track", null = off.
 */
export function SleepTimerMenu({ compact = false }: { compact?: boolean }) {
  const sleepEndsAt = usePlayerStore((s) => s.sleepEndsAt)
  const sleepAfterTrack = usePlayerStore((s) => s.sleepAfterTrack)
  const setSleepTimer = usePlayerStore((s) => s.setSleepTimer)
  const remaining = useSleepCountdown(sleepEndsAt)
  const active = sleepEndsAt !== null || sleepAfterTrack

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          aria-label={active ? `Sleep timer on (${formatCountdown(remaining ?? 0)})` : "Sleep timer"}
          title={active ? "Sleep timer on" : "Sleep timer"}
          className={cn(
            "size-9 rounded-full text-zinc-400 hover:text-zinc-100 hover:bg-zinc-800",
            active && "text-emerald-400 hover:text-emerald-400",
            compact && "size-10"
          )}
        >
          {active ? (
            <MoonStar className="size-4 fill-current sm:size-4" />
          ) : (
            <Moon className={compact ? "size-5" : "size-4"} />
          )}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuLabel className="flex items-center justify-between">
          <span>Sleep timer</span>
          {active && (
            <span className="text-xs font-normal tabular-nums text-emerald-400">
              {sleepAfterTrack ? "end of track" : formatCountdown(remaining ?? 0)}
            </span>
          )}
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        {OPTIONS.map((opt) => (
          <DropdownMenuItem
            key={String(opt.minutes)}
            onClick={() => setSleepTimer(opt.minutes)}
            className={cn("cursor-pointer", opt.minutes === 0 && sleepAfterTrack && "text-emerald-400")}
          >
            {opt.label}
          </DropdownMenuItem>
        ))}
        <DropdownMenuSeparator />
        <DropdownMenuItem
          disabled={!active}
          onClick={() => setSleepTimer(null)}
          className={cn("cursor-pointer", !active && "opacity-40")}
        >
          Turn off
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
