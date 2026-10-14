"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import { Loader2, ZoomIn } from "lucide-react"
import type { AvatarCrop } from "@/lib/api"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

const FRAME = 288

interface Props {
  file: File | null
  busy: boolean
  onCancel: () => void
  onConfirm: (crop: AvatarCrop) => void
  shape?: "circle" | "square"
}

export function AvatarCropDialog({ file, busy, onCancel, onConfirm, shape = "circle" }: Props) {
  const square = shape === "square"
  const [url, setUrl] = useState<string | null>(null)
  const [natural, setNatural] = useState<{ w: number; h: number } | null>(null)
  const [zoom, setZoom] = useState(1)
  const [offset, setOffset] = useState({ x: 0, y: 0 })
  const drag = useRef<{ x: number; y: number; ox: number; oy: number } | null>(null)

  useEffect(() => {
    if (!file) {
      setUrl(null)
      return
    }
    const objectUrl = URL.createObjectURL(file)
    setUrl(objectUrl)
    setNatural(null)
    setZoom(1)
    setOffset({ x: 0, y: 0 })
    return () => URL.revokeObjectURL(objectUrl)
  }, [file])

  const baseScale = natural ? FRAME / Math.min(natural.w, natural.h) : 1
  const drawnW = natural ? natural.w * baseScale * zoom : 0
  const drawnH = natural ? natural.h * baseScale * zoom : 0

  const clamp = useCallback(
    (next: { x: number; y: number }) => ({
      x: Math.min(0, Math.max(FRAME - drawnW, next.x)),
      y: Math.min(0, Math.max(FRAME - drawnH, next.y)),
    }),
    [drawnW, drawnH]
  )

  useEffect(() => {
    setOffset((prev) => clamp(prev))
  }, [clamp])

  const onPointerDown = (e: React.PointerEvent) => {
    ;(e.target as Element).setPointerCapture(e.pointerId)
    drag.current = { x: e.clientX, y: e.clientY, ox: offset.x, oy: offset.y }
  }
  const onPointerMove = (e: React.PointerEvent) => {
    if (!drag.current) return
    setOffset(
      clamp({
        x: drag.current.ox + (e.clientX - drag.current.x),
        y: drag.current.oy + (e.clientY - drag.current.y),
      })
    )
  }
  const onPointerUp = () => {
    drag.current = null
  }

  const confirm = () => {
    if (!natural) return
    const scale = baseScale * zoom
    const shorter = Math.min(natural.w, natural.h)
    onConfirm({
      x: -offset.x / scale / natural.w,
      y: -offset.y / scale / natural.h,
      size: FRAME / scale / shorter,
    })
  }

  return (
    <Dialog open={file !== null} onOpenChange={(open) => !open && !busy && onCancel()}>
      <DialogContent aria-describedby={undefined} className="max-w-md gap-0 overflow-hidden border-zinc-800 bg-zinc-900 p-0 sm:max-w-md">
        <DialogHeader className="border-b border-zinc-800/80 px-5 py-4">
          <DialogTitle className="text-base">{square ? "Position the cover" : "Position your picture"}</DialogTitle>
        </DialogHeader>

        <div className="flex min-w-0 flex-col items-center px-5 py-4">
          <div
            className={cn("relative overflow-hidden bg-zinc-950 ring-1 ring-zinc-700", square ? "rounded-lg" : "rounded-full")}
            style={{ width: FRAME, height: FRAME, touchAction: "none" }}
            onPointerDown={onPointerDown}
            onPointerMove={onPointerMove}
            onPointerUp={onPointerUp}
            onPointerCancel={onPointerUp}
          >
            {url ? (
              <img
                src={url}
                alt={square ? "New cover" : "Your new avatar"}
                draggable={false}
                onLoad={(e) => {
                  const el = e.currentTarget
                  setNatural({ w: el.naturalWidth, h: el.naturalHeight })
                  const s = FRAME / Math.min(el.naturalWidth, el.naturalHeight)
                  setOffset({
                    x: (FRAME - el.naturalWidth * s) / 2,
                    y: (FRAME - el.naturalHeight * s) / 2,
                  })
                }}
                className="max-w-none cursor-grab select-none"
                style={{ width: drawnW || undefined, height: drawnH || undefined, transform: `translate(${offset.x}px, ${offset.y}px)`, transformOrigin: "top left", position: "absolute", left: 0, top: 0 }}
              />
            ) : null}
          </div>

          <div className="mt-4 flex w-full items-center gap-3">
            <ZoomIn className="size-4 shrink-0 text-zinc-500" aria-hidden="true" />
            <input
              type="range"
              min={1}
              max={4}
              step={0.01}
              value={zoom}
              aria-label="Zoom"
              onChange={(e) => setZoom(Number(e.target.value))}
              className="h-1 w-full cursor-pointer appearance-none rounded-full bg-zinc-700 accent-emerald-500"
            />
          </div>
        </div>

        <DialogFooter className="flex-row justify-end gap-2 border-t border-zinc-800/80 px-5 py-3">
          <Button
            variant="ghost"
            size="sm"
            disabled={busy}
            onClick={onCancel}
            className="text-zinc-400 hover:text-zinc-100"
          >
            Cancel
          </Button>
          <Button
            size="sm"
            disabled={busy || !natural}
            onClick={confirm}
            className="bg-emerald-500 font-semibold text-zinc-950 hover:bg-emerald-400"
          >
            {busy ? <Loader2 className="size-4 animate-spin" /> : square ? "Save cover" : "Save picture"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
