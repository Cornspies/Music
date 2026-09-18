"use client"

import type { LucideIcon } from "lucide-react"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"

interface EmptyStateProps {
  icon: LucideIcon
  title: string
  subtitle?: string
  actionLabel?: string
  onAction?: () => void
  compact?: boolean
  className?: string
}

/** Reusable empty state block (icon, title, subtitle, optional action). */
export function EmptyState({
  icon: Icon,
  title,
  subtitle,
  actionLabel,
  onAction,
  compact,
  className,
}: EmptyStateProps) {
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center gap-3 rounded-xl border border-dashed border-zinc-800 bg-zinc-900/40 text-center",
        compact ? "px-6 py-10" : "px-6 py-16",
        className
      )}
    >
      <div className="flex size-14 items-center justify-center rounded-full bg-zinc-800/80 text-zinc-400">
        <Icon className="size-7" aria-hidden="true" />
      </div>
      <div>
        <h3 className="text-base font-semibold text-zinc-200">{title}</h3>
        {subtitle ? <p className="mt-1 max-w-sm text-sm text-zinc-500">{subtitle}</p> : null}
      </div>
      {actionLabel && onAction ? (
        <Button
          onClick={onAction}
          className="bg-emerald-500 text-zinc-950 hover:bg-emerald-400"
        >
          {actionLabel}
        </Button>
      ) : null}
    </div>
  )
}
