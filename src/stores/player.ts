"use client"

import { create } from "zustand"
import { persist } from "zustand/middleware"
import { toast } from "sonner"
import type { PlaybackState, Track } from "@/lib/types"
import { api, coverUrl, fetchPlayback, savePlayback } from "@/lib/api"
import { invoke } from "@tauri-apps/api/core"
import { listen } from "@tauri-apps/api/event"
import { AudioEngine, type Engine, type EngineEvents, trackGain } from "@/lib/audio-engine"
import { isDesktop, serverUrl } from "@/lib/desktop"
import { NativeAudioEngine, type PreviewState } from "@/lib/native-audio"
import { useAuthStore } from "@/stores/auth"
import { useMediaTokenStore } from "@/stores/media-token"

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
  playAll: (tracks: Track[], opts?: { shuffle?: boolean }) => void
  addAllToQueue: (tracks: Track[]) => void
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

export interface SessionHandler {
  togglePlay: () => void
  next: () => void
  prev: () => void
  seek: (sec: number) => void
  jumpTo: (index: number) => void
  playNow: (track: Track) => void
  playNext: (track: Track) => void
  addToQueue: (track: Track) => void
  addAll: (tracks: Track[], mode: "now" | "end") => void
  removeFromQueue: (index: number) => void
  clearQueue: () => void
  trackEnded: (entryId: string) => void
  stoppedHere: () => void
  playHere: (play: boolean) => void
  signedOut: () => void
}

let session: SessionHandler | null = null

export function setSessionHandler(handler: SessionHandler | null) {
  session = handler
  sessionEntry = ""
  sessionNextEntry = ""
  sessionCounted = ""
  const e = getEngine()
  if (!e) return
  e.crossfadeSeconds = handler ? 0 : usePlayerStore.getState().crossfadeSeconds
  if (!handler) prepareNext()
}

export interface SessionSync {
  queue: Track[]
  index: number
  entryId: string
  nextEntryId: string
  sessionPlaying: boolean
  follow: boolean
  position: number
  adopt?: boolean
  jump?: boolean
  tick?: boolean
}

const SESSION_DRIFT_S = 1.5
const SESSION_DRIFT_COOLDOWN_MS = 5000

let sessionEntry = ""
let sessionNextEntry = ""
let sessionCounted = ""
let lastDriftSeekAt = 0

export function followSession(sync: SessionSync): void {
  const e = getEngine()
  if (!e) return
  const track = sync.queue[sync.index] ?? null
  if (!track) {
    if (usePlayerStore.getState().currentTrack) {
      e.stop()
      flushListening()
      listening = null
    }
    sessionEntry = ""
    sessionNextEntry = ""
    usePlayerStore.setState({ queue: sync.queue, queueIndex: -1, currentTrack: null, isPlaying: false, position: 0, duration: 0 })
    return
  }
  const playing = sync.sessionPlaying && sync.follow
  if (sync.jump) lastDriftSeekAt = 0
  if (sync.adopt && sync.entryId !== sessionEntry && e.current?.id === track.id) {
    sessionEntry = sync.entryId
    sessionCounted = sync.entryId
  }
  if (sync.entryId !== sessionEntry) {
    sessionEntry = sync.entryId
    usePlayerStore.setState({
      queue: sync.queue,
      queueIndex: sync.index,
      currentTrack: track,
      position: sync.position,
      duration: track.duration || 0,
      isPlaying: playing,
    })
    e.load(track, { autoplay: playing, startAt: sync.position > SESSION_DRIFT_S ? sync.position : 0 })
    sessionNextEntry = ""
    if (playing) countSessionPlay(track, sync.entryId)
    updateMediaSession(track)
  } else {
    usePlayerStore.setState({ queue: sync.queue, queueIndex: sync.index, currentTrack: track })
    if (playing) {
      const now = Date.now()
      if (Math.abs(e.currentTime - sync.position) > SESSION_DRIFT_S && now - lastDriftSeekAt > SESSION_DRIFT_COOLDOWN_MS) {
        lastDriftSeekAt = now
        e.seek(sync.position)
      }
      if (e.paused && !e.ended && !sync.tick) {
        e.play().catch((err: unknown) => {
          if ((err as Error)?.name !== "AbortError") session?.stoppedHere()
        })
      }
      countSessionPlay(track, sync.entryId)
    } else if (!sync.sessionPlaying) {
      if (!e.paused) e.pause()
      if (Math.abs(e.currentTime - sync.position) > SESSION_DRIFT_S) {
        e.seek(sync.position)
        usePlayerStore.setState({ position: sync.position })
      }
    } else if (!e.paused) {
      e.pause()
    }
  }
  if (sync.nextEntryId !== sessionNextEntry) {
    sessionNextEntry = sync.nextEntryId
    const s = usePlayerStore.getState()
    e.prepareNext(sync.nextEntryId && !s.sleepAfterTrack ? (sync.queue[sync.index + 1] ?? null) : null)
  }
}

function shuffled<T>(items: T[]): T[] {
  const out = [...items]
  for (let i = out.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1))
    ;[out[i], out[j]] = [out[j], out[i]]
  }
  return out
}

function countSessionPlay(track: Track, entryId: string) {
  if (sessionCounted === entryId) return
  sessionCounted = entryId
  reportPlay(track)
}

export function seekHere(sec: number) {
  const e = getEngine()
  if (!e) return
  lastDriftSeekAt = Date.now()
  e.seek(sec)
  usePlayerStore.setState({ position: sec })
}

export function leaveSessionPlayback() {
  setSessionHandler(null)
  const e = getEngine()
  if (e && !e.paused) e.pause()
}

let sleepTimeout: ReturnType<typeof setTimeout> | null = null

function clearSleepTimeout() {
  if (sleepTimeout !== null) {
    clearTimeout(sleepTimeout)
    sleepTimeout = null
  }
}

function armSleepTimer(endsAt: number | null) {
  clearSleepTimeout()
  if (endsAt === null) return
  sleepTimeout = setTimeout(() => {
    const e = getEngine()
    if (e && !e.paused) {
      e.pause()
      toast("Sleep timer ended")
    }
    usePlayerStore.setState({ sleepEndsAt: null })
  }, Math.max(0, endsAt - Date.now()))
}

let engine: Engine | null = null

function getEngine(): Engine | null {
  if (typeof window === "undefined") return null
  if (!engine) {
    const events: EngineEvents = {
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
        if (session && !engine?.ended) session.stoppedHere()
      },
      onEnded: () => {
        const s = usePlayerStore.getState()
        if (session) {
          if (s.sleepAfterTrack) {
            usePlayerStore.setState({ sleepAfterTrack: false })
            toast("Sleep timer")
            session.stoppedHere()
          }
          session.trackEnded(sessionEntry)
          return
        }
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
        if (session) {
          const ended = sessionEntry
          sessionEntry = sessionNextEntry
          sessionNextEntry = ""
          usePlayerStore.setState({
            currentTrack: track,
            queueIndex: s.queueIndex + 1,
            position: 0,
            duration: track.duration || 0,
            isPlaying: true,
          })
          countSessionPlay(track, sessionEntry)
          updateMediaSession(track)
          session.trackEnded(ended)
          return
        }
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
      onError: (message) => {
        const track = usePlayerStore.getState().currentTrack
        if (track) {
          usePlayerStore.setState({ isPlaying: false })
          session?.stoppedHere()
          if (message) toast.error(`Couldn't play “${track.title}”`, { id: "playback-error", description: message.charAt(0).toUpperCase() + message.slice(1) })
        }
      },
    }
    engine = isDesktop ? new NativeAudioEngine(events) : new AudioEngine(events)
    const { volume, muted, crossfadeSeconds, normalizeVolume } = usePlayerStore.getState()
    engine.setVolume(volume, muted)
    engine.crossfadeSeconds = crossfadeSeconds
    engine.normalize = normalizeVolume
    if (engine instanceof NativeAudioEngine) wireNativeMedia(engine)
    else wireMediaSession(engine)
  }
  return engine
}

function reportPlay(track: Track) {
  const flushed = flushListening()
  listening = { trackId: track.id, lastPosition: null, unreported: 0 }
  flushed.then(() => api(`/api/tracks/${track.id}/play`, { method: "POST" })).catch(() => {})
}

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
  if (isDesktop) {
    invoke("media_metadata", {
      title: track.title,
      artist: track.artist || "Unknown artist",
      album: track.album || null,
      coverUrl: track.hasCover ? coverUrl(track, 512) : null,
      duration: track.duration || null,
    }).catch(() => {})
    return
  }
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
  if (isDesktop || typeof navigator === "undefined" || !("mediaSession" in navigator)) return
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

function wireNativeMedia(e: NativeAudioEngine) {
  e.onMediaAction = (action, position) => {
    const s = usePlayerStore.getState()
    switch (action) {
      case "play":
        if (session) return session.playHere(true)
        e.play().catch(() => usePlayerStore.setState({ isPlaying: false }))
        return
      case "pause":
        if (session) return session.playHere(false)
        e.pause()
        return
      case "toggle":
        return s.togglePlay()
      case "next":
        return s.next()
      case "previous":
        return s.prev()
      case "seekBy":
        if (position !== null) s.seek(Math.max(0, e.currentTime + position))
        return
      case "seekTo":
        if (position !== null) s.seek(position)
        return
    }
  }
}

function wireMediaSession(e: Engine) {
  if (!("mediaSession" in navigator)) return
  const ms = navigator.mediaSession
  const trySet = (action: MediaSessionAction, handler: ((details: MediaSessionActionDetails) => void) | null) => {
    try {
      ms.setActionHandler(action, handler)
    } catch {}
  }
  trySet("play", () => {
    if (session) return session.playHere(true)
    e.play().catch(() => usePlayerStore.setState({ isPlaying: false }))
  })
  trySet("pause", () => {
    if (session) return session.playHere(false)
    e.pause()
  })
  trySet("previoustrack", () => usePlayerStore.getState().prev())
  trySet("nexttrack", () => usePlayerStore.getState().next())
  trySet("seekto", (details) => {
    if (typeof details.seekTime === "number") usePlayerStore.getState().seek(details.seekTime)
  })
  trySet("seekbackward", null)
  trySet("seekforward", null)
}

export function playNativePreview(
  track: Track,
  from: number,
  until: number,
  done: (state: PreviewState) => void
): boolean {
  const e = getEngine()
  if (!(e instanceof NativeAudioEngine)) return false
  e.preview(track, from, until, done)
  return true
}

export function stopNativePreview() {
  const e = getEngine()
  if (e instanceof NativeAudioEngine) e.stopPreview()
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

function persistPlayback(opts: { force?: boolean; keepalive?: boolean } = {}): Promise<unknown> {
  if (restoring || session) return Promise.resolve()
  const s = usePlayerStore.getState()
  if (!s.currentTrack || !useAuthStore.getState().token) return Promise.resolve()
  const key = queueKey(s.queue)
  const queueChanged = key !== lastSavedQueueKey
  if (!positionDirty && !queueChanged) return Promise.resolve()
  const now = Date.now()
  if (!opts.force && !queueChanged && now - lastSaveAt < SAVE_INTERVAL_MS) return Promise.resolve()

  lastSaveAt = now
  positionDirty = false
  lastSavedPosition = s.position
  if (queueChanged) lastSavedQueueKey = key
  return savePlayback(
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
  if (isDesktop && (await reattachBackground())) return
  if (usePlayerStore.getState().currentTrack || session) return
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
  if (isDesktop) await reattachBackground()
  if (syncing || restoring || session) return
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
        if (e && !session) e.crossfadeSeconds = crossfadeSeconds
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
        if (session) return session.playNow(track)
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
        if (session) return session.togglePlay()
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
        if (session) return session.next()
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
        if (session) return session.prev()
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
        if (session) return session.seek(sec)
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
        if (!session) prepareNext()
      },

      toggleShuffle: () => {
        set({ shuffle: !get().shuffle })
        if (!session) prepareNext()
      },

      playNext: (track) => {
        if (session) return session.playNext(track)
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
        if (session) return session.addToQueue(track)
        const s = get()
        if (!s.currentTrack) {
          s.playNow(track)
          return
        }
        set({ queue: [...s.queue, track] })
        prepareNext()
      },

      playAll: (tracks, opts = {}) => {
        if (tracks.length === 0) return
        if (session) return session.addAll(opts.shuffle ? shuffled(tracks) : tracks, "now")
        const s = get()
        if (!opts.shuffle) return s.playNow(tracks[0], tracks)
        if (!s.shuffle) s.toggleShuffle()
        s.playNow(tracks[Math.floor(Math.random() * tracks.length)], tracks)
      },

      addAllToQueue: (tracks) => {
        if (tracks.length === 0) return
        if (session) return session.addAll(tracks, "end")
        const s = get()
        if (!s.currentTrack) {
          s.playNow(tracks[0], tracks)
          return
        }
        set({ queue: [...s.queue, ...tracks] })
        prepareNext()
      },

      removeFromQueue: (index) => {
        if (session) return session.removeFromQueue(index)
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
        if (session) return session.jumpTo(index)
        const { queue } = get()
        if (index < 0 || index >= queue.length) return
        playIndex(index)
      },

      clearQueue: () => {
        if (session) return session.clearQueue()
        const { currentTrack } = get()
        set({
          queue: currentTrack ? [currentTrack] : [],
          queueIndex: currentTrack ? 0 : -1,
        })
        prepareNext()
      },

      stop: () => {
        session?.signedOut()
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
        armSleepTimer(endsAt)
        toast(`Sleep timer — ${minutes} min`, {
          description: `Pauses at ${new Date(endsAt).toLocaleTimeString([], {
            hour: "2-digit",
            minute: "2-digit",
          })}`,
        })
      },

      handleTrackDeleted: (id) => {
        if (session) return
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

interface BackgroundResume {
  queue: string
  index: number
  currentId: number
  nextId: number | null
  nextIndex: number | null
  position: number
  duration: number
  playing: boolean
  sleepEndsAt: number | null
  sleepAfterTrack: boolean
  mediaToken: string
  mediaExpiresAt: number
}

let reattaching: Promise<boolean> | null = null

export function reattachBackground(): Promise<boolean> {
  reattaching ??= takeBackground()
  return reattaching
}

async function takeBackground(): Promise<boolean> {
  const e = isDesktop ? getEngine() : null
  if (!(e instanceof NativeAudioEngine)) return false
  await e.ready
  let resume: BackgroundResume | null
  let queue: Track[]
  try {
    resume = await invoke<BackgroundResume | null>("background_take")
    if (!resume) return false
    queue = JSON.parse(resume.queue) as Track[]
  } catch (err) {
    console.error("background_take failed", err)
    return false
  }
  const track = queue[resume.index]
  if (!track) return false
  if (resume.mediaToken) useMediaTokenStore.getState().adopt(resume.mediaToken, resume.mediaExpiresAt)
  const nextTrack = resume.nextIndex !== null ? queue[resume.nextIndex] : undefined
  const next = resume.nextId !== null && nextTrack ? { id: resume.nextId, track: nextTrack } : null
  e.adopt({ id: resume.currentId, track }, next, resume.position, resume.duration, resume.playing)
  pendingNextIndex = next && resume.nextIndex !== null ? resume.nextIndex : -1
  lastSavedQueueKey = queueKey(queue)
  lastSavedPosition = resume.position
  positionDirty = false
  lastSaveAt = Date.now()
  listening = { trackId: track.id, lastPosition: null, unreported: 0 }
  usePlayerStore.setState({
    queue,
    queueIndex: resume.index,
    currentTrack: track,
    position: resume.position,
    duration: resume.duration || track.duration || 0,
    isPlaying: resume.playing,
    sleepEndsAt: resume.sleepEndsAt,
    sleepAfterTrack: resume.sleepAfterTrack,
  })
  armSleepTimer(resume.sleepEndsAt)
  updateMediaSession(track)
  if (!next) prepareNext()
  return true
}

async function handOff(e: NativeAudioEngine): Promise<boolean> {
  const s = usePlayerStore.getState()
  const authToken = useAuthStore.getState().token
  const { current, next } = e.ids
  if (!authToken || current === null || s.queueIndex < 0 || s.queue[s.queueIndex]?.id !== s.currentTrack?.id) return false
  const media = useMediaTokenStore.getState()
  try {
    await invoke("background_start", {
      handoff: {
        server: serverUrl(),
        session: authToken,
        mediaToken: media.token ?? "",
        mediaExpiresAt: media.expiresAt,
        queue: JSON.stringify(s.queue),
        gains: s.queue.map((t) => trackGain(t, s.normalizeVolume)),
        index: s.queueIndex,
        currentId: current,
        nextId: next,
        nextIndex: next !== null && pendingNextIndex >= 0 ? pendingNextIndex : null,
        shuffle: s.shuffle,
        repeat: s.repeat,
        sleepEndsAt: s.sleepEndsAt,
        sleepAfterTrack: s.sleepAfterTrack,
        position: e.currentTime,
        playing: !e.paused,
      },
    })
    return true
  } catch (err) {
    console.error("background_start failed", err)
    return false
  }
}

async function handleCloseRequest(keepWebview: boolean, closeToTray: boolean) {
  const e = engine
  const playing = e instanceof NativeAudioEngine && !e.paused && usePlayerStore.getState().currentTrack !== null
  if (!closeToTray) {
    await Promise.all([flushListening(), persistPlayback({ force: true })])
    await invoke("app_quit").catch((err: unknown) => console.error("app_quit failed", err))
    return
  }
  if (session || (playing && keepWebview)) {
    await invoke("window_hide").catch((err: unknown) => console.error("window_hide failed", err))
    return
  }
  if (playing && e instanceof NativeAudioEngine) {
    void flushListening(true)
    void persistPlayback({ force: true, keepalive: true })
    if (await handOff(e)) return
    e.pause()
  } else {
    await Promise.all([flushListening(), persistPlayback({ force: true })])
  }
  await invoke("window_tray").catch((err: unknown) => console.error("window_tray failed", err))
}

if (isDesktop && typeof window !== "undefined") {
  listen<{ keepWebview: boolean; closeToTray?: boolean }>("close-requested", (event) => {
    void handleCloseRequest(event.payload.keepWebview, event.payload.closeToTray ?? false)
  }).catch((err: unknown) => console.error("listening for close requests failed", err))
}

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
