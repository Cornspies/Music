"use client"

import { Fragment, useCallback, useEffect, useRef, useState, useSyncExternalStore, type ReactNode } from "react"

const OVERSCAN_ROWS = 2

function subscribeResize(notify: () => void) {
  window.addEventListener("resize", notify)
  return () => window.removeEventListener("resize", notify)
}

export function useColumns(base: number, steps: ReadonlyArray<readonly [number, number]>): number {
  return useSyncExternalStore(
    subscribeResize,
    () => steps.reduce((columns, [minWidth, n]) => (window.innerWidth >= minWidth ? n : columns), base),
    () => base
  )
}

export function VirtualGrid<T>({
  items,
  columns,
  gap,
  itemKey,
  renderItem,
}: {
  items: T[]
  columns: number
  gap: number
  itemKey: (item: T) => string
  renderItem: (item: T) => ReactNode
}) {
  const listRef = useRef<HTMLDivElement>(null)
  const rowsRef = useRef<HTMLDivElement>(null)
  const [rowHeight, setRowHeight] = useState(240)
  const [range, setRange] = useState({ start: 0, end: 4 })
  const rowCount = Math.ceil(items.length / columns)
  const stride = rowHeight + gap

  const measure = useCallback(() => {
    const list = listRef.current
    const main = list?.closest("main")
    if (!list || !main) return
    const above = main.getBoundingClientRect().top - list.getBoundingClientRect().top
    const start = Math.max(0, Math.floor(above / stride) - OVERSCAN_ROWS)
    const end = Math.min(rowCount, Math.ceil((above + main.clientHeight) / stride) + OVERSCAN_ROWS)
    setRange((r) => (r.start === start && r.end === end ? r : { start, end }))
  }, [stride, rowCount])

  useEffect(() => {
    const main = listRef.current?.closest("main")
    if (!main) return
    const frame = requestAnimationFrame(measure)
    main.addEventListener("scroll", measure, { passive: true })
    const resize = new ResizeObserver(measure)
    resize.observe(main)
    return () => {
      cancelAnimationFrame(frame)
      main.removeEventListener("scroll", measure)
      resize.disconnect()
    }
  }, [measure])

  useEffect(() => {
    const rows = rowsRef.current
    if (!rows) return
    const resize = new ResizeObserver(() => {
      const h = rows.firstElementChild?.getBoundingClientRect().height
      if (h) setRowHeight((prev) => (Math.abs(prev - h) > 0.5 ? h : prev))
    })
    resize.observe(rows)
    return () => resize.disconnect()
  }, [])

  const rows: T[][] = []
  for (let r = range.start; r < Math.min(range.end, rowCount); r++) {
    rows.push(items.slice(r * columns, r * columns + columns))
  }

  return (
    <div ref={listRef} className="relative" style={{ height: rowCount > 0 ? rowCount * stride - gap : 0 }}>
      <div
        ref={rowsRef}
        className="absolute inset-x-0 top-0 flex flex-col"
        style={{ gap, transform: `translateY(${range.start * stride}px)` }}
      >
        {rows.map((row, i) => (
          <div
            key={range.start + i}
            className="grid"
            style={{ gap, gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))` }}
          >
            {row.map((item) => (
              <Fragment key={itemKey(item)}>{renderItem(item)}</Fragment>
            ))}
          </div>
        ))}
      </div>
    </div>
  )
}
