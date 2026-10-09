"use client"

import { useCallback, useState, type ReactNode } from "react"
import { createPortal } from "react-dom"
import { DropdownMenu, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"

export interface MenuPoint {
  x: number
  y: number
}

export function useContextMenu() {
  const [open, setOpen] = useState(false)
  const [point, setPoint] = useState<MenuPoint | null>(null)
  const onContextMenu = useCallback((e: React.MouseEvent) => {
    e.preventDefault()
    const at = { x: e.clientX, y: e.clientY }
    const show = () => {
      window.removeEventListener("pointerup", show)
      window.removeEventListener("pointercancel", show)
      setPoint(at)
      setOpen(true)
    }
    if (e.buttons === 0) {
      show()
      return
    }
    window.addEventListener("pointerup", show)
    window.addEventListener("pointercancel", show)
  }, [])
  const onOpenChange = useCallback((next: boolean) => {
    setOpen(next)
    if (next) setPoint(null)
  }, [])
  return { open, point, onOpenChange, onContextMenu }
}

export function MenuAtPoint({
  point,
  onOpenChange,
  children,
}: {
  point: MenuPoint
  onOpenChange: (open: boolean) => void
  children: ReactNode
}) {
  return createPortal(
    <DropdownMenu open onOpenChange={onOpenChange}>
      <DropdownMenuTrigger asChild>
        <span
          aria-hidden="true"
          className="pointer-events-none fixed size-0"
          style={{ left: point.x, top: point.y }}
        />
      </DropdownMenuTrigger>
      {children}
    </DropdownMenu>,
    document.body
  )
}
