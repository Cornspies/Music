"use client"

// Player store — THE HEART. A single module-level AudioEngine (two <audio>
// elements, see lib/audio-engine.ts) shared by the whole app, driven by a
// zustand store. Volume and playback settings are persisted ("music-volume").

import { create } from "zustand"
import { persist } from "zustand/middleware"
import { toast } from "sonner"
import type { PlaybackState, Track } from "@/lib/types"
import { api, coverUrl, fetchPlayback, savePlayback } from "@/lib/api"
import { AudioEngine } from "@/lib/audio-engine"
import { useAuthStore } from "@/stores/auth"

export type RepeatMode = "off" | "all" | "one"

/** Crossfade lengths offered in the playback settings menu, in seconds. */
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
  /** seconds of overlap between tracks; 0 = gapless (next track pre-buffered) */
  crossfadeSeconds: number
  setCrossfade: (seconds: number) => void
  /** level tracks to a common loudness using the server's analysis */
  normalizeVolume: boolean
  setNormalizeVolume: (on: boolean) => void
}

// module-level handle for the pending sleep timeout
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
        positionDirty = true
        persistPlayback()
      },
      onDuration: (duration) => {
        usePlayerStore.setState({ duration })
        updatePositionState()
      },
      onPlay: () => {
        usePlayerStore.setState({ isPlaying: true })
        updatePositionState()
        // A track restored from the last session resumes without a new play
        // event; its listening still counts against the play it belongs to.
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
        } else {
          s.next()
        }
      },
      onAdvanced: (track) => {
        // The engine started the prepared track on its own (gapless or
        // crossfade); catch the queue up to it.
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
  // A new play: whatever the previous track still had unreported goes out
  // first, against that track.
  flushListening()
  listening = { trackId: track.id, lastPosition: null, unreported: 0 }
  api(`/api/tracks/${track.id}/play`, { method: "POST" }).catch(() => {
    // play-count reporting is best-effort
  })
}

// ---- listening time ---------------------------------------------------------
//
// The history's "time listened" counts what was actually heard, not track
// lengths: playback progress is accumulated here in small steps (a jump of
// more than a couple of seconds is a seek, not listening) and sent to the
// server every so often, on pause, on track change and when the page goes
// away. A lost report costs at most one interval.

const LISTEN_REPORT_INTERVAL_S = 15

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

function flushListening(keepalive = false) {
  const l = listening
  if (!l || l.unreported < 0.5 || !useAuthStore.getState().token) return
  const seconds = Math.round(l.unreported * 10) / 10
  l.unreported = 0
  api(`/api/tracks/${l.trackId}/listened`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ seconds }),
    keepalive,
  }).catch(() => {
    // best-effort; the next report carries on from here
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
  } catch {
    // ignore
  }
}

function wireMediaSession(e: AudioEngine) {
  if (!("mediaSession" in navigator)) return
  const ms = navigator.mediaSession
  const trySet = (action: MediaSessionAction, handler: ((details: MediaSessionActionDetails) => void) | null) => {
    try {
      ms.setActionHandler(action, handler)
    } catch {
      // action unsupported on this browser
    }
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

// Index of the track the engine has been told to buffer as "next", or -1.
// Chosen once per track so shuffle picks the same song for the pre-buffer
// and for a manual skip.
let pendingNextIndex = -1

/** Works out what follows the current track and lets the engine buffer it. */
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

/** Load queue[index] into the engine and play it. */
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
  const e = getEngine()
  if (e) {
    e.load(state.track, { autoplay: false, startAt: state.position })
    prepareNext()
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
          // single track: loop if repeating, otherwise just restart
          const e = getEngine()
          if (e) {
            e.seek(0)
            if (repeat !== "off" || usePlayerStore.getState().isPlaying) {
              e.play().catch(() => set({ isPlaying: false }))
            }
          }
          return
        }
        let nextIndex: number
        if (pendingNextIndex >= 0 && pendingNextIndex < queue.length) {
          // Whatever was pre-buffered — the same choice a crossfade would make.
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
  // pagehide covers tab close, navigation and bfcache; visibilitychange
  // covers switching apps on mobile, where pagehide may never come.
  window.addEventListener("pagehide", () => {
    flushListening(true)
    persistPlayback({ force: true, keepalive: true })
  })
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "hidden") {
      flushListening(true)
      persistPlayback({ force: true, keepalive: true })
    }
  })
  // Queue edits (add / remove / reorder) are rare; write them straight away.
  // Deferred a microtask so that playNow — which sets the queue and then the
  // track in two updates — produces one save carrying both, not two.
  usePlayerStore.subscribe((s, prev) => {
    if (s.queue !== prev.queue) queueMicrotask(() => persistPlayback({ force: true }))
  })
}
