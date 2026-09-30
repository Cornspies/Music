"use client"

import { create } from "zustand"
import { persist } from "zustand/middleware"
import { toast } from "sonner"
import type { PlaybackState, Track } from "@/lib/types"
import { api, coverUrl, fetchPlayback, savePlayback } from "@/lib/api"
import { AudioEngine } from "@/lib/audio-engine"
import { useAuthStore } from "@/stores/auth"

export type RepeatMode = "off" | "all" | "one"

export const CROSSFADE_OPTIONS = [0, 2, 4, 8, 12] as const

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
  handleTrackDeleted: (id: string) => void
  replaceTrack: (track: Track) => void
  setSleepTimer: (minutes: number | null) => void
  sleepEndsAt: number | null
  sleepAfterTrack: boolean
  crossfadeSeconds: number
  setCrossfade: (seconds: number) => void
  normalizeVolume: boolean
  setNormalizeVolume: (on: boolean) => void
}

let sleepTimeout: ReturnType<typeof setTimeout> | null = null

function clearSleepTimeout() {
  if (sleepTimeout !== null) {
    clearTimeout(sleepTimeout)
    sleepTimeout = null
  }
}

let engine: AudioEngine | null = null

function getEngine(): AudioEngine | null {
  if (typeof window === "undefined") return null
  if (!engine) {
    engine = new AudioEngine({
      onTime: (position) => {
        noteListening(position)
        usePlayerStore.setState({ position })
        if (position % 5 < 0.3) updatePositionState()
        if (Math.abs(position - lastSavedPosition) >= 1) positionDirty = true
        persistPlayback()
      },
      onDuration: (duration) => {
        usePlayerStore.setState({ duration })
        updatePositionState()
      },
      onPlay: () => {
        usePlayerStore.setState({ isPlaying: true })
        updatePositionState()
        const cur = usePlayerStore.getState().currentTrack
        if (cur && listening?.trackId !== cur.id) {
          flushListening()
          listening = { trackId: cur.id, lastPosition: null, unreported: 0 }
        }
      },
      onPause: () => {
        usePlayerStore.setState({ isPlaying: false })
        flushListening()
        persistPlayback({ force: true })
      },
      onEnded: () => {
        const s = usePlayerStore.getState()
        if (s.sleepAfterTrack) {
          usePlayerStore.setState({ sleepAfterTrack: false, isPlaying: false })
          toast("Sleep timer")
          return
        }
        if (s.repeat === "one" && s.currentTrack) {
          engine?.seek(0)
          engine?.play().catch(() => usePlayerStore.setState({ isPlaying: false }))
          reportPlay(s.currentTrack)
        } else {
          s.next()
        }
      },
      onAdvanced: (track) => {
        const s = usePlayerStore.getState()
        const index = pendingNextIndex >= 0 && s.queue[pendingNextIndex]?.id === track.id
          ? pendingNextIndex
          : s.queue.findIndex((t) => t.id === track.id)
        pendingNextIndex = -1
        usePlayerStore.setState({
          currentTrack: track,
          queueIndex: index,
          position: 0,
          duration: track.duration || 0,
          isPlaying: true,
        })
        reportPlay(track)
        updateMediaSession(track)
        positionDirty = true
        persistPlayback({ force: true })
        prepareNext()
      },
      onError: () => {
        if (usePlayerStore.getState().currentTrack) {
          usePlayerStore.setState({ isPlaying: false })
        }
      },
    })
    const { volume, muted, crossfadeSeconds, normalizeVolume } = usePlayerStore.getState()
    engine.setVolume(volume, muted)
    engine.crossfadeSeconds = crossfadeSeconds
    engine.normalize = normalizeVolume
    wireMediaSession(engine)
  }
  return engine
}

function reportPlay(track: Track) {
  // The server adds listening time to the latest play of a track, so the
  // previous play's time has to arrive first (repeat-one plays it again).
  const flushed = flushListening()
  listening = { trackId: track.id, lastPosition: null, unreported: 0 }
  flushed.then(() => api(`/api/tracks/${track.id}/play`, { method: "POST" })).catch(() => {})
}

// Listening time is also reported on pause, track changes and when the page is
// hidden; this only bounds what an uninterrupted session could lose. Frequent
// requests keep a phone's radio awake.
const LISTEN_REPORT_INTERVAL_S = 5 * 60

let listening: { trackId: string; lastPosition: number | null; unreported: number } | null = null

function noteListening(position: number) {
  const l = listening
  if (!l || usePlayerStore.getState().currentTrack?.id !== l.trackId) return
  if (l.lastPosition !== null) {
    const delta = position - l.lastPosition
    if (delta > 0 && delta < 2) l.unreported += delta
  }
  l.lastPosition = position
  if (l.unreported >= LISTEN_REPORT_INTERVAL_S) flushListening()
}

function flushListening(keepalive = false): Promise<unknown> {
  const l = listening
  if (!l || l.unreported < 0.5 || !useAuthStore.getState().token) return Promise.resolve()
  const seconds = Math.round(l.unreported * 10) / 10
  l.unreported = 0
  return api(`/api/tracks/${l.trackId}/listened`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ seconds }),
    keepalive,
  }).catch(() => {})
}

function updateMediaSession(track: Track) {
  if (typeof navigator === "undefined" || !("mediaSession" in navigator)) return
  try {
    navigator.mediaSession.metadata = new MediaMetadata({
      title: track.title,
      artist: track.artist || "Unknown artist",
      album: track.album || "",
      artwork: track.hasCover ? [{ src: coverUrl(track, 512), sizes: "512x512" }] : [],
    })
  } catch {}
}

function updatePositionState() {
  if (typeof navigator === "undefined" || !("mediaSession" in navigator)) return
  if (!engine) return
  try {
    const duration = engine.duration
    if (duration > 0) {
      navigator.mediaSession.setPositionState({
        duration,
        playbackRate: 1,
        position: Math.min(engine.currentTime, duration),
      })
    }
  } catch {}
}

function wireMediaSession(e: AudioEngine) {
  if (!("mediaSession" in navigator)) return
  const ms = navigator.mediaSession
  const trySet = (action: MediaSessionAction, handler: ((details: MediaSessionActionDetails) => void) | null) => {
    try {
      ms.setActionHandler(action, handler)
    } catch {}
  }
  trySet("play", () => {
    e.play().catch(() => usePlayerStore.setState({ isPlaying: false }))
  })
  trySet("pause", () => e.pause())
  trySet("previoustrack", () => usePlayerStore.getState().prev())
  trySet("nexttrack", () => usePlayerStore.getState().next())
  trySet("seekto", (details) => {
    if (typeof details.seekTime === "number") usePlayerStore.getState().seek(details.seekTime)
  })
  trySet("seekbackward", null)
  trySet("seekforward", null)
}

let pendingNextIndex = -1

function prepareNext() {
  const e = getEngine()
  if (!e) return
  const s = usePlayerStore.getState()
  const { queue, queueIndex, shuffle, repeat } = s
  let next = -1
  if (!s.sleepAfterTrack && repeat !== "one" && queue.length > 1) {
    if (shuffle) {
      do {
        next = Math.floor(Math.random() * queue.length)
      } while (next === queueIndex)
    } else if (queueIndex + 1 < queue.length) {
      next = queueIndex + 1
    } else if (repeat === "all") {
      next = 0
    }
  }
  pendingNextIndex = next
  e.prepareNext(next >= 0 ? queue[next] : null)
}

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
  const e = getEngine()
  if (!e) return
  e.load(track, { autoplay: true })
  reportPlay(track)
  updateMediaSession(track)
  positionDirty = true
  persistPlayback({ force: true })
  prepareNext()
}

// The position is also saved on pause, track changes and when the page is
// hidden, which is what resuming on another device relies on; this interval
// is only a fallback for long uninterrupted playback.
const SAVE_INTERVAL_MS = 5 * 60_000
const SYNC_INTERVAL_MS = 60_000
let positionDirty = false
let lastSaveAt = 0
let lastSavedPosition = 0
let lastSavedQueueKey = ""
let knownUpdatedAt = ""
let restoring = false
let syncing = false

function queueKey(queue: Track[]): string {
  return queue.map((t) => t.id).join(",")
}

function noteUpdatedAt(stamp: string | undefined) {
  if (stamp && stamp > knownUpdatedAt) knownUpdatedAt = stamp
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
  lastSavedPosition = s.position
  if (queueChanged) lastSavedQueueKey = key
  savePlayback(
    {
      trackId: s.currentTrack.id,
      position: s.position,
      ...(queueChanged ? { queue: s.queue.map((t) => t.id) } : {}),
    },
    opts.keepalive
  )
    .then((r) => noteUpdatedAt(r?.updatedAt))
    .catch(() => {
      positionDirty = true
      if (queueChanged) lastSavedQueueKey = ""
    })
}

function nothingUnsaved(): boolean {
  const s = usePlayerStore.getState()
  return !positionDirty && (!s.currentTrack || queueKey(s.queue) === lastSavedQueueKey)
}

function applyServerState(state: PlaybackState) {
  const s = usePlayerStore.getState()
  const same =
    s.currentTrack?.id === state.track.id &&
    Math.abs(s.position - state.position) < 1 &&
    queueKey(s.queue) === queueKey(state.queue)
  if (!same) {
    restoring = true
    usePlayerStore.setState({
      queue: state.queue,
      queueIndex: state.queueIndex,
      currentTrack: state.track,
      position: state.position,
      duration: state.track.duration || 0,
      isPlaying: false,
    })
    const e = getEngine()
    if (e) {
      e.load(state.track, { autoplay: false, startAt: state.position })
      prepareNext()
    }
    updateMediaSession(state.track)
    restoring = false
  }
  lastSavedQueueKey = queueKey(state.queue)
  lastSavedPosition = state.position
  positionDirty = false
  lastSaveAt = Date.now()
  noteUpdatedAt(state.updatedAt)
}

export async function restorePlayback(): Promise<void> {
  if (usePlayerStore.getState().currentTrack) return
  let state: PlaybackState | null
  try {
    state = await fetchPlayback()
  } catch {
    return
  }
  if (!state || usePlayerStore.getState().currentTrack) return
  applyServerState(state)
}

async function syncPlayback(): Promise<void> {
  if (syncing || restoring) return
  const s = usePlayerStore.getState()
  if (s.isPlaying || !useAuthStore.getState().token || !nothingUnsaved()) return
  syncing = true
  try {
    const state = await fetchPlayback(knownUpdatedAt || undefined)
    const now = usePlayerStore.getState()
    if (state && !now.isPlaying && nothingUnsaved()) applyServerState(state)
  } catch {} finally {
    syncing = false
  }
}

function rewindCurrent() {
  const e = getEngine()
  if (e) {
    e.pause()
    e.seek(0)
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
      crossfadeSeconds: 0,
      normalizeVolume: true,

      setCrossfade: (seconds) => {
        const crossfadeSeconds = Math.max(0, Math.min(12, seconds))
        const e = getEngine()
        if (e) e.crossfadeSeconds = crossfadeSeconds
        set({ crossfadeSeconds })
      },

      setNormalizeVolume: (on) => {
        const e = getEngine()
        if (e) {
          e.normalize = on
          e.refreshNormalization()
        }
        set({ normalizeVolume: on })
      },

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
        const e = getEngine()
        if (!e) return
        if (e.paused) {
          e.play().catch(() => set({ isPlaying: false }))
        } else {
          e.pause()
        }
      },

      next: () => {
        const { queue, queueIndex, shuffle, repeat } = get()
        if (queue.length === 0) return
        if (queue.length === 1) {
          const e = getEngine()
          if (e) {
            e.seek(0)
            if (repeat !== "off" || usePlayerStore.getState().isPlaying) {
              e.play().catch(() => set({ isPlaying: false }))
              reportPlay(queue[0])
            }
          }
          return
        }
        let nextIndex: number
        if (pendingNextIndex >= 0 && pendingNextIndex < queue.length) {
          nextIndex = pendingNextIndex
        } else if (shuffle) {
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
        const e = getEngine()
        if (e && e.currentTime > 3) {
          e.seek(0)
          set({ position: 0 })
          return
        }
        let prevIndex = s.queueIndex - 1
        if (prevIndex < 0) {
          if (s.repeat === "all" && s.queue.length > 0) prevIndex = s.queue.length - 1
          else {
            if (e) e.seek(0)
            set({ position: 0 })
            return
          }
        }
        playIndex(prevIndex)
      },

      seek: (sec) => {
        const s = get()
        if (!s.currentTrack) return
        const e = getEngine()
        const dur = e && e.duration > 0 ? e.duration : s.duration
        const clamped = Math.max(0, Math.min(sec, dur > 0 ? dur : sec))
        if (e) e.seek(clamped)
        set({ position: clamped })
      },

      setVolume: (v) => {
        const volume = Math.max(0, Math.min(1, v))
        const muted = volume === 0 ? get().muted : false
        getEngine()?.setVolume(volume, muted)
        set({ volume, muted })
      },

      toggleMute: () => {
        const muted = !get().muted
        getEngine()?.setVolume(get().volume, muted)
        set({ muted })
      },

      cycleRepeat: () => {
        const order: RepeatMode[] = ["off", "all", "one"]
        const current = get().repeat
        set({ repeat: order[(order.indexOf(current) + 1) % order.length] })
        prepareNext()
      },

      toggleShuffle: () => {
        set({ shuffle: !get().shuffle })
        prepareNext()
      },

      playNext: (track) => {
        const s = get()
        if (!s.currentTrack) {
          s.playNow(track)
          return
        }
        const queue = [...s.queue]
        queue.splice(s.queueIndex + 1, 0, track)
        set({ queue })
        prepareNext()
      },

      addToQueue: (track) => {
        const s = get()
        if (!s.currentTrack) {
          s.playNow(track)
          return
        }
        set({ queue: [...s.queue, track] })
        prepareNext()
      },

      removeFromQueue: (index) => {
        const { queue, queueIndex } = get()
        if (index < 0 || index >= queue.length || index === queueIndex) return
        const newQueue = queue.filter((_, i) => i !== index)
        set({
          queue: newQueue,
          queueIndex: index < queueIndex ? queueIndex - 1 : queueIndex,
        })
        prepareNext()
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
        prepareNext()
      },

      stop: () => {
        getEngine()?.stop()
        pendingNextIndex = -1
        set({
          currentTrack: null,
          queue: [],
          queueIndex: -1,
          isPlaying: false,
          position: 0,
          duration: 0,
        })
        positionDirty = false
        lastSavedQueueKey = ""
        if (typeof navigator !== "undefined" && "mediaSession" in navigator) {
          try {
            navigator.mediaSession.metadata = null
          } catch {}
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
          set({ sleepEndsAt: null, sleepAfterTrack: true })
          prepareNext()
          toast("Sleep timer set")
          return
        }
        const endsAt = Date.now() + minutes * 60_000
        set({ sleepEndsAt: endsAt, sleepAfterTrack: false })
        sleepTimeout = setTimeout(() => {
          const e = getEngine()
          if (e && !e.paused) {
            e.pause()
            toast("Sleep timer ended")
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
          prepareNext()
        }
      },

      replaceTrack: (track) => {
        const { queue, currentTrack } = get()
        set({
          queue: queue.map((t) => (t.id === track.id ? track : t)),
          currentTrack: currentTrack?.id === track.id ? track : currentTrack,
        })
        getEngine()?.replaceTrack(track)
        if (currentTrack?.id === track.id) updateMediaSession(track)
      },
    }),
    {
      name: "music-volume",
      partialize: (s) => ({
        volume: s.volume,
        shuffle: s.shuffle,
        repeat: s.repeat,
        crossfadeSeconds: s.crossfadeSeconds,
        normalizeVolume: s.normalizeVolume,
      }),
    }
  )
)

if (typeof window !== "undefined") {
  window.addEventListener("pagehide", () => {
    flushListening(true)
    persistPlayback({ force: true, keepalive: true })
  })
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "hidden") {
      flushListening(true)
      persistPlayback({ force: true, keepalive: true })
    } else {
      syncPlayback()
    }
  })
  window.addEventListener("focus", () => syncPlayback())
  setInterval(() => {
    if (document.visibilityState === "visible") syncPlayback()
  }, SYNC_INTERVAL_MS)
  usePlayerStore.subscribe((s, prev) => {
    if (s.queue !== prev.queue) queueMicrotask(() => persistPlayback({ force: true }))
  })
}
