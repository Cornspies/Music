"use client"

import { create } from "zustand"
import { toast } from "sonner"
import { ApiError, listen as listenApi } from "@/lib/api"
import type { ListenEntry, ListenState, Track } from "@/lib/types"
import { useAuthStore } from "@/stores/auth"
import {
  followSession,
  leaveSessionPlayback,
  seekHere,
  setSessionHandler,
  usePlayerStore,
  type SessionHandler,
} from "@/stores/player"

// Listen together. The server keeps the session's timeline; this device
// follows it through a long poll and plays the audio itself. Whoever may
// control the session (the host, or everyone if the host allows it) changes
// what plays with commands. Nobody pauses for everyone: pausing stops this
// device only, and playing again jumps to where everyone else is.

interface ListenStore {
  session: ListenState | null
  entries: ListenEntry[]
  /** Paused on this device while the session plays on. */
  localPaused: boolean
  dialogOpen: boolean
  setDialogOpen: (open: boolean) => void
  /** An invite link waiting for its join prompt. */
  invite: string | null
  setInvite: (code: string | null) => void
}

export const useListenStore = create<ListenStore>()((set) => ({
  session: null,
  entries: [],
  localPaused: false,
  dialogOpen: false,
  setDialogOpen: (open) => set({ dialogOpen: open }),
  invite: null,
  setInvite: (code) => set({ invite: code }),
}))

export const MAX_LISTEN_GUESTS = 20
/** Songs queued after the current one, at most. */
export const MAX_LISTEN_QUEUE = 1000

const SYNC_INTERVAL_MS = 3000

let receivedAt = 0
let queueTracks: Track[] = []
let pollAbort: AbortController | null = null
let syncTimer: ReturnType<typeof setInterval> | null = null
let knownMembers: Set<string> | null = null
// States at or below this version predate a change made on this device and
// would undo it for a moment.
let optimisticVersion = -1
// Set while this device leaves, so the poll noticing doesn't announce it too.
let leaving = false

export function inListenSession(): boolean {
  return useListenStore.getState().session !== null
}

function myId(): string | undefined {
  return useAuthStore.getState().user?.id
}

export function canRemoveEntry(session: ListenState, entry: ListenEntry): boolean {
  return session.canControl || entry.addedBy.id === myId()
}

function expectedPosition(st: ListenState): number {
  const pos = st.playing ? st.position + (performance.now() - receivedAt) / 1000 : st.position
  const duration = useListenStore.getState().entries.find((e) => e.id === st.current)?.track.duration ?? 0
  return duration > 0 ? Math.min(pos, duration) : pos
}

function sync(opts: { adopt?: boolean; jump?: boolean; tick?: boolean } = {}) {
  const { session: st, entries, localPaused } = useListenStore.getState()
  if (!st) return
  const index = entries.findIndex((e) => e.id === st.current)
  followSession({
    queue: queueTracks,
    index,
    entryId: index >= 0 ? st.current : "",
    nextEntryId: index >= 0 ? (entries[index + 1]?.id ?? "") : "",
    sessionPlaying: st.playing,
    follow: !localPaused,
    position: expectedPosition(st),
    ...opts,
  })
}

function apply(state: ListenState, adopt = false) {
  const current = useListenStore.getState()
  const same = current.session?.id === state.id
  if (same && (state.version < current.session!.version || state.version <= optimisticVersion)) return
  optimisticVersion = -1
  let entries = same ? current.entries : []
  if (state.entries) {
    entries = state.entries
    queueTracks = entries.map((e) => e.track)
  } else if (!same) {
    queueTracks = []
  }
  receivedAt = performance.now()
  announceMembers(state)
  useListenStore.setState({ session: { ...state, entries: undefined }, entries })
  sync({ adopt })
}

function announceMembers(state: ListenState) {
  const ids = new Set(state.members.map((m) => m.id))
  const before = knownMembers
  knownMembers = ids
  if (!before) return
  const me = myId()
  for (const m of state.members) {
    if (!before.has(m.id) && m.id !== me) toast(`${m.username} joined the session`)
  }
  if (before.size > ids.size) {
    const left = [...before].filter((id) => !ids.has(id) && id !== me).length
    if (left === 1) toast("Someone left the session")
    else if (left > 1) toast(`${left} people left the session`)
  }
}

async function pollLoop(id: string, signal: AbortSignal) {
  let backoff = 1000
  while (!signal.aborted) {
    const st = useListenStore.getState().session
    if (!st || st.id !== id) return
    try {
      const next = await listenApi.poll(id, st.version, st.queueVersion, signal)
      if (signal.aborted) return
      apply(next)
      backoff = 1000
    } catch (e) {
      if (signal.aborted) return
      if (gone(e)) return
      if (!useAuthStore.getState().token) return
      await new Promise((r) => setTimeout(r, backoff))
      backoff = Math.min(backoff * 2, 15_000)
    }
  }
}

/** Handles the server saying this device isn't in the session any more. */
function gone(e: unknown): boolean {
  if (!(e instanceof ApiError) || e.status !== 404) return false
  const reason = (e.data as { reason?: string } | null)?.reason
  if (!reason) return false
  if (leaving) {
    exit()
    return true
  }
  const st = useListenStore.getState().session
  exit(
    reason === "removed"
      ? "The host removed you from the listening session"
      : reason === "left"
        ? "You left the listening session"
        : reason === "timeout"
          ? st?.isHost
            ? "Your listening session ended while you were away"
            : `The listening session ended: ${st?.host.username ?? "the host"} was away too long`
          : st && !st.isHost
            ? `${st.host.username} ended the listening session`
            : "The listening session ended",
  )
  return true
}

function enter(state: ListenState, followNow: boolean) {
  stopFollowing()
  knownMembers = null
  optimisticVersion = -1
  useListenStore.setState({ session: null, entries: [], localPaused: !followNow })
  setSessionHandler(handler)
  apply(state, true)
  pollAbort = new AbortController()
  void pollLoop(state.id, pollAbort.signal)
  syncTimer = setInterval(() => {
    const { session: st, localPaused } = useListenStore.getState()
    if (st?.playing && !localPaused) sync({ tick: true })
  }, SYNC_INTERVAL_MS)
}

function stopFollowing() {
  pollAbort?.abort()
  pollAbort = null
  if (syncTimer !== null) clearInterval(syncTimer)
  syncTimer = null
}

function exit(message?: string) {
  stopFollowing()
  const was = useListenStore.getState().session
  useListenStore.setState({ session: null, entries: [], localPaused: false })
  queueTracks = []
  knownMembers = null
  if (was) leaveSessionPlayback()
  if (message) toast(message)
}

/** Sends a change and applies the session it answers with. */
async function command(
  send: (id: string, queue: number) => Promise<ListenState>,
  opts: { quiet?: boolean } = {},
): Promise<boolean> {
  const st = useListenStore.getState().session
  if (!st) return false
  try {
    apply(await send(st.id, st.queueVersion))
    return true
  } catch (e) {
    optimisticVersion = -1
    if (gone(e)) return false
    if (!opts.quiet) toast.error((e as Error).message)
    void refresh()
    return false
  }
}

async function refresh() {
  const st = useListenStore.getState().session
  if (!st) return
  try {
    apply(await listenApi.poll(st.id, 0, st.queueVersion))
  } catch (e) {
    gone(e)
  }
}

function denied(what: string) {
  const st = useListenStore.getState().session
  if (st) toast(`Only ${st.host.username} can ${what} in this session`)
}

/** Plays or pauses this device only; playing jumps to where the session is. */
function followHere(play: boolean) {
  useListenStore.setState({ localPaused: !play })
  sync({ jump: play })
}

/** The session only stops when its queue runs out. */
function queueEnded() {
  const st = useListenStore.getState().session
  if (st) toast(st.canControl ? "The queue has ended. Add a song to keep going." : "The queue has ended.")
}

/** Queues songs after the current one ("next"), at the end, or to play right away ("now"). */
async function add(tracks: Track[], mode: "end" | "next" | "now") {
  const { session: st, entries } = useListenStore.getState()
  if (!st || tracks.length === 0) return
  if (!st.canControl) return denied("choose what plays")
  const room = MAX_LISTEN_QUEUE - (entries.length - (entries.findIndex((e) => e.id === st.current) + 1))
  const adding = tracks.slice(0, Math.max(0, room))
  if (adding.length === 0) return toast(`The queue is full (${MAX_LISTEN_QUEUE} songs)`)
  const ok = await command((id, q) => listenApi.add(id, q, adding.map((t) => t.id), mode))
  if (!ok) return
  const left = tracks.length - adding.length
  if (left > 0) toast(`Added ${adding.length} songs. The queue is full, so the other ${left} didn't fit.`)
  else if (adding.length > 1) toast.success(`Added ${adding.length} songs to the queue`)
  else if (mode === "next") toast.success(`Playing “${adding[0].title}” next`)
  else if (mode === "end") toast.success(`Added “${adding[0].title}” to the queue`)
}

const handler: SessionHandler = {
  togglePlay: () => {
    const { session: st, localPaused } = useListenStore.getState()
    if (!st) return
    if (!st.playing) return queueEnded()
    followHere(localPaused || !usePlayerStore.getState().isPlaying)
  },
  next: () => {
    if (!useListenStore.getState().session?.canControl) return denied("skip")
    void command((id, q) => listenApi.control(id, q, { action: "next" }))
  },
  prev: () => {
    if (!useListenStore.getState().session?.canControl) return denied("skip")
    void command((id, q) => listenApi.control(id, q, { action: "previous" }))
  },
  seek: (sec) => {
    const st = useListenStore.getState().session
    if (!st) return
    if (!st.canControl) return denied("skip around")
    optimisticVersion = st.version
    receivedAt = performance.now()
    useListenStore.setState({ session: { ...st, position: sec } })
    seekHere(sec)
    void command((id, q) => listenApi.control(id, q, { action: "seek", position: sec }))
  },
  jumpTo: (index) => {
    const { session: st, entries } = useListenStore.getState()
    const entry = entries[index]
    if (!st || !entry) return
    if (!st.canControl) return denied("choose what plays")
    void command((id, q) => listenApi.control(id, q, { action: "jump", entryId: entry.id }))
  },
  playNow: (track) => void add([track], "now"),
  playNext: (track) => void add([track], "next"),
  addToQueue: (track) => void add([track], "end"),
  addAll: (tracks, mode) => void add(tracks, mode),
  removeFromQueue: (index) => {
    const { session: st, entries } = useListenStore.getState()
    const entry = entries[index]
    if (!st || !entry) return
    if (!canRemoveEntry(st, entry)) return denied("remove other people's songs")
    void command((id, q) => listenApi.remove(id, q, entry.id))
  },
  clearQueue: () => {
    if (!useListenStore.getState().session?.isHost) return denied("clear the queue")
    void command((id, q) => listenApi.clear(id, q))
  },
  trackEnded: (entryId) => {
    if (!entryId) return
    void command((id, q) => listenApi.control(id, q, { action: "ended", entryId }), { quiet: true })
  },
  stoppedHere: () => {
    const { session: st, localPaused } = useListenStore.getState()
    if (st?.playing && !localPaused) useListenStore.setState({ localPaused: true })
  },
  playHere: (play) => {
    const st = useListenStore.getState().session
    if (!st) return
    if (play && !st.playing) return queueEnded()
    followHere(play)
  },
  signedOut: () => exit(),
}

/** Starts a session with what's playing now and the queue after it. */
export async function startListening(opts: { capacity: number; everyoneCanControl: boolean }) {
  const p = usePlayerStore.getState()
  const upcoming = p.currentTrack ? p.queue.slice(p.queueIndex + 1).map((t) => t.id) : []
  const state = await listenApi.create({
    ...opts,
    trackId: p.currentTrack?.id,
    position: p.position,
    upcoming: upcoming.slice(0, MAX_LISTEN_QUEUE),
  })
  enter(state, true)
}

export async function joinListening(code: string) {
  enter(await listenApi.join(code), true)
}

/** Picks up the session this account is in, e.g. after a reload. It stays paused here until played. */
export async function restoreListening(): Promise<boolean> {
  try {
    const state = await listenApi.current()
    if (!state) return false
    enter(state, false)
    return true
  } catch {
    return false
  }
}

export async function leaveListening() {
  const st = useListenStore.getState().session
  if (!st) return
  leaving = true
  try {
    await (st.isHost ? listenApi.end(st.id) : listenApi.leave(st.id))
  } catch (e) {
    if (!(e instanceof ApiError && e.status === 404)) {
      toast.error((e as Error).message)
      return
    }
  } finally {
    leaving = false
  }
  exit(st.isHost ? "You ended the listening session" : "You left the listening session")
}

export async function updateListening(patch: { capacity?: number; everyoneCanControl?: boolean }) {
  await command((id, q) => listenApi.settings(id, q, patch))
}

export async function removeListener(userId: string) {
  await command((id, q) => listenApi.removeMember(id, q, userId))
}

// An invite link survives signing in (and switching to "create account").
const INVITE_KEY = "listen-invite"

export function rememberListenInvite(code: string) {
  try {
    sessionStorage.setItem(INVITE_KEY, code)
  } catch {}
}

export function pendingListenInvite(): string | null {
  try {
    return sessionStorage.getItem(INVITE_KEY)
  } catch {
    return null
  }
}

export function forgetListenInvite() {
  try {
    sessionStorage.removeItem(INVITE_KEY)
  } catch {}
}

if (typeof window !== "undefined") {
  // Signing out here stops following; the account stays in the session, which
  // its other devices may still be playing.
  useAuthStore.subscribe((s, prev) => {
    if (prev.token && !s.token) exit()
  })
}
