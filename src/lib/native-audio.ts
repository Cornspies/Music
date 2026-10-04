import { Channel, invoke } from "@tauri-apps/api/core"
import { nativeStreamUrl } from "@/lib/api"
import { type Engine, type EngineEvents, trackGain } from "@/lib/audio-engine"
import type { Track } from "@/lib/types"
import { useMediaTokenStore } from "@/stores/media-token"

type NativeEvent =
  | { type: "time"; id: number; position: number }
  | { type: "duration"; id: number; duration: number }
  | { type: "play"; id: number }
  | { type: "pause"; id: number }
  | { type: "ended"; id: number }
  | { type: "advanced"; id: number }
  | { type: "error"; id: number; message: string; expired: boolean }
  | { type: "media"; action: MediaAction; position: number | null }
  | { type: "preview"; state: PreviewState }

export type MediaAction = "play" | "pause" | "toggle" | "next" | "previous" | "seekBy" | "seekTo"

export type PreviewState = "playing" | "ended" | "error"

export interface Loaded {
  id: number
  track: Track
}

function send(command: string, args?: Record<string, unknown>) {
  invoke(command, args).catch((err: unknown) => console.error(`${command} failed`, err))
}

export class NativeAudioEngine implements Engine {
  private seq = 0
  private cur: Loaded | null = null
  private nxt: Loaded | null = null
  private pos = 0
  private posAt = 0
  private dur = 0
  private playing = false
  private finished = false
  private fade = 0
  private retriedTrack: string | null = null
  private previewDone: ((state: PreviewState) => void) | null = null
  normalize = false
  onMediaAction: ((action: MediaAction, position: number | null) => void) | null = null
  readonly ready: Promise<void>

  constructor(private events: EngineEvents) {
    const channel = new Channel<NativeEvent>()
    channel.onmessage = (event) => this.handle(event)
    this.ready = invoke("audio_init", { events: channel }).then(
      () => undefined,
      (err: unknown) => console.error("audio_init failed", err)
    )
  }

  get ids(): { current: number | null; next: number | null } {
    return { current: this.cur?.id ?? null, next: this.nxt?.id ?? null }
  }

  adopt(current: Loaded, next: Loaded | null, position: number, duration: number, playing: boolean): void {
    this.cur = current
    this.nxt = next
    this.pos = position
    this.posAt = performance.now()
    this.dur = duration || current.track.duration || 0
    this.playing = playing
    this.finished = false
    this.seq = Math.max(this.seq, current.id, next?.id ?? 0)
  }

  get current(): Track | null {
    return this.cur?.track ?? null
  }

  get currentTime(): number {
    if (!this.playing) return this.pos
    const now = this.pos + (performance.now() - this.posAt) / 1000
    return this.dur > 0 ? Math.min(now, this.dur) : now
  }

  get duration(): number {
    return this.dur
  }

  get paused(): boolean {
    return !this.playing
  }

  get ended(): boolean {
    return this.finished
  }

  get crossfadeSeconds(): number {
    return this.fade
  }

  set crossfadeSeconds(seconds: number) {
    this.fade = seconds
    send("audio_crossfade", { seconds })
  }

  load(track: Track, opts: { autoplay: boolean; startAt?: number } = { autoplay: true }): void {
    const startAt = opts.startAt && opts.startAt > 0 ? opts.startAt : 0
    const promote = this.nxt !== null && this.nxt.track.id === track.id && startAt === 0
    const id = promote && this.nxt ? this.nxt.id : ++this.seq
    this.cur = { id, track }
    this.nxt = null
    this.pos = startAt
    this.posAt = performance.now()
    this.dur = track.duration || 0
    this.playing = opts.autoplay
    this.finished = false
    send("audio_load", {
      id,
      url: nativeStreamUrl(track),
      startAt,
      autoplay: opts.autoplay,
      gain: trackGain(track, this.normalize),
    })
  }

  prepareNext(track: Track | null): void {
    if (!track) {
      if (this.nxt) {
        this.nxt = null
        send("audio_prepare", { id: null, url: null, gain: 1 })
      }
      return
    }
    if (this.nxt?.track.id === track.id) return
    const id = ++this.seq
    this.nxt = { id, track }
    send("audio_prepare", { id, url: nativeStreamUrl(track), gain: trackGain(track, this.normalize) })
  }

  async play(): Promise<void> {
    if (!this.cur) return
    this.playing = true
    this.finished = false
    this.posAt = performance.now()
    await invoke("audio_play")
  }

  pause(): void {
    if (this.playing) this.pos = this.currentTime
    this.playing = false
    send("audio_pause")
  }

  seek(seconds: number): void {
    this.pos = Math.max(0, seconds)
    this.posAt = performance.now()
    this.finished = false
    send("audio_seek", { position: this.pos })
  }

  setVolume(volume: number, muted: boolean): void {
    send("audio_volume", { volume, muted })
  }

  refreshNormalization(): void {
    send("audio_gains", {
      current: this.cur ? trackGain(this.cur.track, this.normalize) : 1,
      next: this.nxt ? trackGain(this.nxt.track, this.normalize) : 1,
    })
  }

  replaceTrack(track: Track): void {
    if (this.cur?.track.id === track.id) this.cur = { ...this.cur, track }
    if (this.nxt?.track.id === track.id) this.nxt = { ...this.nxt, track }
  }

  stop(): void {
    this.cur = null
    this.nxt = null
    this.playing = false
    this.finished = false
    this.pos = 0
    this.dur = 0
    send("audio_stop")
  }

  preview(track: Track, from: number, until: number, done: (state: PreviewState) => void): void {
    this.previewDone = done
    send("audio_preview", { url: nativeStreamUrl(track), start: from, until })
  }

  stopPreview(): void {
    this.previewDone = null
    send("audio_preview_stop")
  }

  private handle(event: NativeEvent) {
    if (event.type === "media") {
      this.onMediaAction?.(event.action, event.position)
      return
    }
    if (event.type === "preview") {
      if (event.state === "playing") return
      const done = this.previewDone
      this.previewDone = null
      done?.(event.state)
      return
    }
    if (event.type === "advanced") {
      const next = this.nxt
      if (!next || next.id !== event.id) return
      this.cur = next
      this.nxt = null
      this.pos = 0
      this.posAt = performance.now()
      this.dur = next.track.duration || 0
      this.playing = true
      this.finished = false
      this.events.onAdvanced(next.track)
      return
    }
    const cur = this.cur
    if (!cur || cur.id !== event.id) return
    switch (event.type) {
      case "time":
        this.pos = event.position
        this.posAt = performance.now()
        this.events.onTime(event.position)
        break
      case "duration":
        this.dur = event.duration
        this.events.onDuration(event.duration)
        break
      case "play":
        this.playing = true
        this.posAt = performance.now()
        this.events.onPlay()
        break
      case "pause":
        this.playing = false
        this.events.onPause()
        break
      case "ended":
        this.playing = false
        this.finished = true
        this.events.onEnded()
        break
      case "error": {
        const wasPlaying = this.playing
        this.playing = false
        if (event.expired && this.retriedTrack !== cur.track.id) {
          this.retriedTrack = cur.track.id
          const at = this.pos
          void useMediaTokenStore
            .getState()
            .refresh()
            .then(() => {
              if (this.cur?.id === cur.id) this.load(cur.track, { autoplay: wasPlaying, startAt: at })
            })
          return
        }
        this.events.onError()
        break
      }
    }
  }
}
