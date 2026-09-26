"use client"

import { useEffect } from "react"
import { usePlayerStore } from "@/stores/player"
import { useLibraryStore } from "@/stores/library"
import { useUIStore } from "@/stores/ui"

const SLIDER_KEYS = new Set(["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown", "Home", "End", "PageUp", "PageDown"])

export function useKeyboardShortcuts() {
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.isComposing || e.ctrlKey || e.metaKey || e.altKey) return
      const target = e.target instanceof Element ? e.target : null
      const typing =
        target !== null &&
        (target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.tagName === "SELECT" ||
          (target instanceof HTMLElement && target.isContentEditable))
      if (typing) return
      if (SLIDER_KEYS.has(e.key) && target?.closest('[data-slot="slider"], [role="slider"]')) return

      const overlay =
        document.querySelector('[role="dialog"]:not([data-shortcuts-allowed])') !== null ||
        document.querySelector('[role="menu"]') !== null
      if (overlay) return

      const player = usePlayerStore.getState()
      const ui = useUIStore.getState()

      switch (e.key) {
        case " ":
          if (e.repeat) return
          e.preventDefault()
          player.togglePlay()
          break
        case "ArrowLeft":
          e.preventDefault()
          player.seek(Math.max(0, player.position - 10))
          break
        case "ArrowRight":
          e.preventDefault()
          player.seek(player.position + 10)
          break
        case "ArrowUp":
          e.preventDefault()
          player.setVolume(player.volume + 0.05)
          break
        case "ArrowDown":
          e.preventDefault()
          player.setVolume(player.volume - 0.05)
          break
        case "m":
        case "M":
          player.toggleMute()
          break
        case "n":
        case "N":
          player.next()
          break
        case "p":
        case "P":
          player.prev()
          break
        case "s":
        case "S":
          player.toggleShuffle()
          break
        case "r":
        case "R":
          player.cycleRepeat()
          break
        case "f":
        case "F": {
          const current = usePlayerStore.getState().currentTrack
          if (current) useLibraryStore.getState().toggleFavorite(current.id)
          break
        }
        case "q":
        case "Q":
          ui.setQueueOpen(!ui.queueOpen)
          break
        case "o":
        case "O":
          ui.setNowPlayingOpen(!ui.nowPlayingOpen)
          break
        case "/":
          e.preventDefault()
          ui.navigate("search")
          window.dispatchEvent(new CustomEvent("music:focus-search"))
          break
        case "?":
          e.preventDefault()
          ui.setShortcutsOpen(true)
          break
      }
    }
    window.addEventListener("keydown", onKeyDown)
    return () => window.removeEventListener("keydown", onKeyDown)
  }, [])
}
