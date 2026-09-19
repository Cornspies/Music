"use client"

// Player store — THE HEART. A single module-level HTMLAudioElement shared by
// the whole app, driven by a zustand store. Volume is persisted ("music-volume").

import { create } from "zustand"
import { persist } from "zustand/middleware"
import { toast } from "sonner"
import type { PlaybackState, Track } from "@/lib/types"
import { api, coverUrl, fetchPlayback, savePlayback, streamUrl } from "@/lib/api"
import { useAuthStore } from "@/stores/auth"

export type RepeatMode = "off" | "all" | "one"

interface PlayerState {
  currentTrack: Track | null
  queue: Track[]
  queueIndex: number
  isPlaying: boolean
  position: number
  duration: number
  volume: number
  muted: boolean
  shuffle: boolean
  repeat: RepeatMode
  playNow: (track: Track, contextQueue?: Track[]) => void
  togglePlay: () => void
  next: () => void
  prev: () => void
  seek: (sec: number) => void
  setVolume: (v: number) => void
  toggleMute: () => void
  cycleRepeat: () => void
  toggleShuffle: () => void
  playNext: (track: Track) => void
  addToQueue: (track: Track) => void
  removeFromQueue: (index: number) => void
  jumpTo: (index: number) => void
  clearQueue: () => void
  stop: () => void
  /** housekeeping when a track is deleted from the library */
  handleTrackDeleted: (id: string) => void
  /** swap metadata for an edited track everywhere (queue + current) */
  replaceTrack: (track: Track) => void
  /** sleep timer: minutes from now, or null to cancel. 0 = end of current track. */
  setSleepTimer: (minutes: number | null) => void
  /** epoch-ms when the sleep timer fires (null when off) */
  sleepEndsAt: number | null
  /** true → stop playback at the end of the current track */
  sleepAfterTrack: boolean
}

// module-level handle for the pending sleep timeout
let sleepTimeout: ReturnType<typeof setTimeout> | null = null

function clearSleepTimeout() {
  if (sleepTimeout !== null) {
    clearTimeout(sleepTimeout)
    sleepTimeout = null
  }
}

let audio: HTMLAudioElement | null = null

function getAudio(): HTMLAudioElement | null {
  if (typeof window === "undefined") return null
  if (!audio) {
    audio = new Audio()
    audio.preload = "metadata"
    const { volume, muted } = usePlayerStore.getState()
    audio.volume = volume
    audio.muted = muted
    wireEvents(audio)
  }
  return audio
}

function reportPlay(track: Track) {
  api(`/api/tracks/${track.id}/play`, { method: "POST" }).catch(() => {
    // play-count reporting is best-effort
  })
}

function updateMediaSession(track: Track) {
  if (typeof navigator === "undefined" || !("mediaSession" in navigator)) return
  try {
    navigator.mediaSession.metadata = new MediaMetadata({
      title: track.title,
      artist: track.artist || "Unknown artist",
      album: track.album || "",
      artwork: track.hasCover ? [{ src: coverUrl(track), sizes: "512x512" }] : [],
    })
  } catch {
    // MediaMetadata not supported
  }
}

function updatePositionState() {
  if (typeof navigator === "undefined" || !("mediaSession" in navigator)) return
  if (!audio) return
  try {
    if (Number.isFinite(audio.duration)) {
      navigator.mediaSession.setPositionState({
        duration: audio.duration,
        playbackRate: audio.playbackRate,
        position: Math.min(audio.currentTime, audio.duration),
      })
    }
  } catch {
    // ignore
  }
}

function wireEvents(a: HTMLAudioElement) {
  a.addEventListener("timeupdate", () => {
    usePlayerStore.setState({ position: a.currentTime })
    if (a.currentTime % 5 < 0.3) updatePositionState()
    positionDirty = true
    persistPlayback()
  })
  // A seek while paused changes what "continue listening" means, but the
  // page-hide / pause saves pick it up; no need for a request per scrub.
  a.addEventListener("seeked", () => {
    positionDirty = true
  })
  a.addEventListener("loadedmetadata", () => {
    if (Number.isFinite(a.duration)) usePlayerStore.setState({ duration: a.duration })
    updatePositionState()
  })
  a.addEventListener("durationchange", () => {
    if (Number.isFinite(a.duration)) usePlayerStore.setState({ duration: a.duration })
  })
  a.addEventListener("play", () => {
    usePlayerStore.setState({ isPlaying: true })
    updatePositionState()
  })
  a.addEventListener("playing", () => usePlayerStore.setState({ isPlaying: true }))
  a.addEventListener("pause", () => {
    usePlayerStore.setState({ isPlaying: false })
    persistPlayback({ force: true })
  })
  a.addEventListener("ended", () => {
    const s = usePlayerStore.getState()
    if (s.sleepAfterTrack) {
      usePlayerStore.setState({ sleepAfterTrack: false, isPlaying: false })
      toast("Sleep timer — stopped after track", {
        description: `${s.currentTrack?.title ?? "Playback"} finished. Good night 🌙`,
      })
      return
    }
    if (s.repeat === "one" && s.currentTrack) {
      a.currentTime = 0
      a.play().catch(() => usePlayerStore.setState({ isPlaying: false }))
    } else {
      s.next()
    }
  })
  a.addEventListener("error", () => {
    if (usePlayerStore.getState().currentTrack) {
      usePlayerStore.setState({ isPlaying: false })
    }
  })

  if ("mediaSession" in navigator) {
    const ms = navigator.mediaSession
    const trySet = (action: MediaSessionAction, handler: ((details: MediaSessionActionDetails) => void) | null) => {
      try {
        ms.setActionHandler(action, handler)
      } catch {
        // action unsupported on this browser
      }
    }
    trySet("play", () => {
      a.play().catch(() => usePlayerStore.setState({ isPlaying: false }))
    })
    trySet("pause", () => a.pause())
    trySet("previoustrack", () => usePlayerStore.getState().prev())
    trySet("nexttrack", () => usePlayerStore.getState().next())
    trySet("seekto", (details) => {
      if (typeof details.seekTime === "number") usePlayerStore.getState().seek(details.seekTime)
    })
    trySet("seekbackward", null)
    trySet("seekforward", null)
  }
}

/** Load queue[index] into the audio element and play it. */
function playIndex(index: number) {
  const s = usePlayerStore.getState()
  const track = s.queue[index]
  if (!track) return
  usePlayerStore.setState({
    currentTrack: track,
    queueIndex: index,
    position: 0,
    duration: track.duration || 0,
    isPlaying: true,
  })
  const a = getAudio()
  if (!a) return
  a.src = streamUrl(track)
  a.play().catch(() => usePlayerStore.setState({ isPlaying: false }))
  reportPlay(track)
  updateMediaSession(track)
  positionDirty = true
  persistPlayback({ force: true })
}

// ---- continue listening ------------------------------------------------------
//
// The server remembers what each user last had loaded (track, position, up-next
// queue), so a closed tab or a different device can pick up where they left
// off. Saved every few seconds while playing, immediately on pause and on
// track change, and with a keepalive request as the page goes away. Only
// state that actually changed here is written, so a tab left paused for hours
// never overwrites what another device has been doing since.

const SAVE_INTERVAL_MS = 10_000
let positionDirty = false
let lastSaveAt = 0
let lastSavedQueueKey = ""
let restoring = false

function queueKey(queue: Track[]): string {
  return queue.map((t) => t.id).join(",")
}

function persistPlayback(opts: { force?: boolean; keepalive?: boolean } = {}) {
  if (restoring) return
  const s = usePlayerStore.getState()
  if (!s.currentTrack || !useAuthStore.getState().token) return
  const key = queueKey(s.queue)
  const queueChanged = key !== lastSavedQueueKey
  if (!positionDirty && !queueChanged) return
  const now = Date.now()
  if (!opts.force && !queueChanged && now - lastSaveAt < SAVE_INTERVAL_MS) return

  lastSaveAt = now
  positionDirty = false
  // Marked as saved up front so a second call in the same tick doesn't send
  // the queue twice; a failed request resets both so the next save resends.
  if (queueChanged) lastSavedQueueKey = key
  savePlayback(
    {
      trackId: s.currentTrack.id,
      position: s.position,
      ...(queueChanged ? { queue: s.queue.map((t) => t.id) } : {}),
    },
    opts.keepalive
  ).catch(() => {
    positionDirty = true
    if (queueChanged) lastSavedQueueKey = ""
  })
}

/**
 * Loads the server-remembered track into the player, paused, at the saved
 * position — never auto-playing: browsers block it, and a tab opened by
 * accident shouldn't start making noise. Skipped when this session already
 * has something loaded.
 */
export async function restorePlayback(): Promise<void> {
  if (usePlayerStore.getState().currentTrack) return
  let state: PlaybackState | null
  try {
    state = await fetchPlayback()
  } catch {
    return
  }
  if (!state || usePlayerStore.getState().currentTrack) return

  restoring = true
  usePlayerStore.setState({
    queue: state.queue,
    queueIndex: state.queueIndex,
    currentTrack: state.track,
    position: state.position,
    duration: state.track.duration || 0,
    isPlaying: false,
  })
  const a = getAudio()
  if (a) {
    a.src = streamUrl(state.track)
    // The seek has to wait for metadata: before that, some browsers ignore
    // a currentTime assignment outright.
    const seek = () => {
      a.currentTime = state.position
      a.removeEventListener("loadedmetadata", seek)
    }
    a.addEventListener("loadedmetadata", seek)
  }
  updateMediaSession(state.track)
  // What we just loaded is exactly what the server has — nothing to write
  // until the user actually does something.
  lastSavedQueueKey = queueKey(state.queue)
  positionDirty = false
  lastSaveAt = Date.now()
  restoring = false
}

function rewindCurrent() {
  const a = getAudio()
  if (a) {
    a.pause()
    a.currentTime = 0
  }
  usePlayerStore.setState({ isPlaying: false, position: 0 })
}

export const usePlayerStore = create<PlayerState>()(
  persist(
    (set, get) => ({
      currentTrack: null,
      queue: [],
      queueIndex: -1,
      isPlaying: false,
      position: 0,
      duration: 0,
      volume: 0.8,
      muted: false,
      shuffle: false,
      repeat: "off",
      sleepEndsAt: null,
      sleepAfterTrack: false,

      playNow: (track, contextQueue) => {
        const s = get()
        let queue: Track[]
        if (contextQueue && contextQueue.length > 0) {
          const idx = contextQueue.findIndex((t) => t.id === track.id)
          queue = idx === -1 ? [track, ...contextQueue] : contextQueue
        } else if (s.queue.some((t) => t.id === track.id)) {
          queue = s.queue
        } else {
          queue = [track]
        }
        const index = Math.max(0, queue.findIndex((t) => t.id === track.id))
        set({ queue, queueIndex: index })
        playIndex(index)
      },

      togglePlay: () => {
        const s = get()
        if (!s.currentTrack) return
        const a = getAudio()
        if (!a) return
        if (a.paused) {
          a.play().catch(() => set({ isPlaying: false }))
        } else {
          a.pause()
        }
      },

      next: () => {
        const { queue, queueIndex, shuffle, repeat } = get()
        if (queue.length === 0) return
        if (queue.length === 1) {
          // single track: loop if repeating, otherwise just restart
          const a = getAudio()
          if (a) {
            a.currentTime = 0
            if (repeat !== "off" || usePlayerStore.getState().isPlaying) {
              a.play().catch(() => set({ isPlaying: false }))
            }
          }
          return
        }
        let nextIndex: number
        if (shuffle) {
          do {
            nextIndex = Math.floor(Math.random() * queue.length)
          } while (nextIndex === queueIndex && queue.length > 1)
        } else {
          nextIndex = queueIndex + 1
        }
        if (nextIndex >= queue.length) {
          if (repeat === "all") nextIndex = 0
          else {
            rewindCurrent()
            return
          }
        }
        playIndex(nextIndex)
      },

      prev: () => {
        const s = get()
        if (!s.currentTrack) return
        const a = getAudio()
        if (a && a.currentTime > 3) {
          a.currentTime = 0
          set({ position: 0 })
          return
        }
        let prevIndex = s.queueIndex - 1
        if (prevIndex < 0) {
          if (s.repeat === "all" && s.queue.length > 0) prevIndex = s.queue.length - 1
          else {
            if (a) a.currentTime = 0
            set({ position: 0 })
            return
          }
        }
        playIndex(prevIndex)
      },

      seek: (sec) => {
        const s = get()
        if (!s.currentTrack) return
        const a = getAudio()
        const dur = a && Number.isFinite(a.duration) ? a.duration : s.duration
        const clamped = Math.max(0, Math.min(sec, dur > 0 ? dur : sec))
        if (a) a.currentTime = clamped
        set({ position: clamped })
      },

      setVolume: (v) => {
        const volume = Math.max(0, Math.min(1, v))
        const muted = volume === 0 ? get().muted : false
        const a = getAudio()
        if (a) {
          a.volume = volume
          a.muted = muted
        }
        set({ volume, muted })
      },

      toggleMute: () => {
        const muted = !get().muted
        const a = getAudio()
        if (a) a.muted = muted
        set({ muted })
      },

      cycleRepeat: () => {
        const order: RepeatMode[] = ["off", "all", "one"]
        const current = get().repeat
        set({ repeat: order[(order.indexOf(current) + 1) % order.length] })
      },

      toggleShuffle: () => set({ shuffle: !get().shuffle }),

      playNext: (track) => {
        const s = get()
        if (!s.currentTrack) {
          s.playNow(track)
          return
        }
        const queue = [...s.queue]
        queue.splice(s.queueIndex + 1, 0, track)
        set({ queue })
      },

      addToQueue: (track) => {
        const s = get()
        if (!s.currentTrack) {
          s.playNow(track)
          return
        }
        set({ queue: [...s.queue, track] })
      },

      removeFromQueue: (index) => {
        const { queue, queueIndex } = get()
        if (index < 0 || index >= queue.length || index === queueIndex) return
        const newQueue = queue.filter((_, i) => i !== index)
        set({
          queue: newQueue,
          queueIndex: index < queueIndex ? queueIndex - 1 : queueIndex,
        })
      },

      jumpTo: (index) => {
        const { queue } = get()
        if (index < 0 || index >= queue.length) return
        playIndex(index)
      },

      clearQueue: () => {
        const { currentTrack } = get()
        set({
          queue: currentTrack ? [currentTrack] : [],
          queueIndex: currentTrack ? 0 : -1,
        })
      },

      stop: () => {
        const a = getAudio()
        if (a) {
          a.pause()
          a.removeAttribute("src")
          a.load()
        }
        set({
          currentTrack: null,
          queue: [],
          queueIndex: -1,
          isPlaying: false,
          position: 0,
          duration: 0,
        })
        // Local bookkeeping only. The server's copy is left alone: stop() also
        // runs on logout and when the playing track was deleted, and neither
        // should forget the position for the user's other devices.
        positionDirty = false
        lastSavedQueueKey = ""
        if (typeof navigator !== "undefined" && "mediaSession" in navigator) {
          try {
            navigator.mediaSession.metadata = null
          } catch {
            // ignore
          }
        }
      },

      setSleepTimer: (minutes) => {
        clearSleepTimeout()
        if (minutes === null) {
          set({ sleepEndsAt: null, sleepAfterTrack: false })
          toast("Sleep timer off")
          return
        }
        if (minutes === 0) {
          // 0 = "end of current track"
          set({ sleepEndsAt: null, sleepAfterTrack: true })
          toast("Sleep timer set — end of track", {
            description: "Playback stops when the current track finishes.",
          })
          return
        }
        const endsAt = Date.now() + minutes * 60_000
        set({ sleepEndsAt: endsAt, sleepAfterTrack: false })
        sleepTimeout = setTimeout(() => {
          const a = getAudio()
          if (a && !a.paused) {
            a.pause()
            toast("Sleep timer ended", {
              description: "Playback paused. Good night 🌙",
            })
          }
          usePlayerStore.setState({ sleepEndsAt: null })
        }, minutes * 60_000)
        toast(`Sleep timer — ${minutes} min`, {
          description: `Pauses at ${new Date(endsAt).toLocaleTimeString([], {
            hour: "2-digit",
            minute: "2-digit",
          })}`,
        })
      },

      handleTrackDeleted: (id) => {
        const { queue, queueIndex, currentTrack } = get()
        const idx = queue.findIndex((t) => t.id === id)
        const inCurrent = currentTrack?.id === id
        if (idx === -1 && !inCurrent) return
        const newQueue = queue.filter((t) => t.id !== id)
        if (inCurrent) {
          if (newQueue.length === 0) {
            get().stop()
            return
          }
          const nextIdx = Math.min(Math.max(idx, 0), newQueue.length - 1)
          set({ queue: newQueue })
          playIndex(nextIdx)
        } else {
          set({
            queue: newQueue,
            queueIndex: idx < queueIndex ? queueIndex - 1 : queueIndex,
          })
        }
      },

      replaceTrack: (track) => {
        const { queue, currentTrack } = get()
        set({
          queue: queue.map((t) => (t.id === track.id ? track : t)),
          currentTrack: currentTrack?.id === track.id ? track : currentTrack,
        })
        if (currentTrack?.id === track.id) updateMediaSession(track)
      },
    }),
    {
      name: "music-volume",
      partialize: (s) => ({ volume: s.volume, shuffle: s.shuffle, repeat: s.repeat }),
    }
  )
)

if (typeof window !== "undefined") {
  // pagehide covers tab close, navigation and bfcache; visibilitychange
  // covers switching apps on mobile, where pagehide may never come.
  window.addEventListener("pagehide", () => persistPlayback({ force: true, keepalive: true }))
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "hidden") persistPlayback({ force: true, keepalive: true })
  })
  // Queue edits (add / remove / reorder) are rare; write them straight away.
  // Deferred a microtask so that playNow — which sets the queue and then the
  // track in two updates — produces one save carrying both, not two.
  usePlayerStore.subscribe((s, prev) => {
    if (s.queue !== prev.queue) queueMicrotask(() => persistPlayback({ force: true }))
  })
}
