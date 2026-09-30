import type { Track } from "@/lib/types"
import { streamUrl } from "@/lib/api"

const TARGET_LUFS = -14
const MAX_BOOST_DB = 3
const MIN_GAIN = 0.1

export interface EngineEvents {
  onTime: (position: number) => void
  onDuration: (duration: number) => void
  onPlay: () => void
  onPause: () => void
  onEnded: () => void
  onAdvanced: (track: Track) => void
  onError: () => void
}

interface Slot {
  el: HTMLAudioElement
  gain: GainNode | null
  track: Track | null
  trackGain: number
  fade: number
}

export class AudioEngine {
  private slots: [Slot, Slot]
  private active = 0
  private ctx: AudioContext | null = null
  private ctxTried = false
  private volume = 1
  private muted = false
  private fading = false
  private fadeRaf: number | null = null
  private handedOff = new WeakSet<HTMLAudioElement>()

  crossfadeSeconds = 0
  normalize = false

  private deferredNext: Track | null | undefined = undefined

  constructor(private events: EngineEvents) {
    this.slots = [this.makeSlot(0), this.makeSlot(1)]
  }

  get current(): Track | null {
    return this.slots[this.active].track
  }

  get currentTime(): number {
    return this.slots[this.active].el.currentTime
  }

  get duration(): number {
    const d = this.slots[this.active].el.duration
    return Number.isFinite(d) ? d : 0
  }

  get paused(): boolean {
    return this.slots[this.active].el.paused
  }

  /** Played to the end; play() would start it over. */
  get ended(): boolean {
    return this.slots[this.active].el.ended
  }

  load(track: Track, opts: { autoplay: boolean; startAt?: number } = { autoplay: true }): void {
    this.deferredNext = undefined
    this.cancelFade()
    const other = this.slots[1 - this.active]
    const cur = this.slots[this.active]

    if (other.track?.id === track.id && other.el.src) {
      this.silence(cur)
      this.active = 1 - this.active
      this.applyGain(other, 1)
      this.startSlot(other, opts)
      return
    }

    this.silence(other)
    other.track = null
    other.el.removeAttribute("src")

    cur.track = track
    cur.trackGain = this.gainFor(track)
    cur.fade = 1
    cur.el.src = streamUrl(track)
    cur.el.load()
    this.applyGain(cur, 1)
    this.startSlot(cur, opts)
  }

  prepareNext(track: Track | null): void {
    if (this.fading) {
      this.deferredNext = track
      return
    }
    const other = this.slots[1 - this.active]
    if (!track) {
      if (other.track) {
        this.silence(other)
        other.track = null
        other.el.removeAttribute("src")
      }
      return
    }
    if (other.track?.id === track.id && other.el.src) return
    this.silence(other)
    other.track = track
    other.trackGain = this.gainFor(track)
    other.fade = 0
    other.el.preload = "auto"
    other.el.src = streamUrl(track)
    other.el.load()
  }

  play(): Promise<void> {
    this.resumeContext()
    return this.slots[this.active].el.play()
  }

  pause(): void {
    this.cancelFade()
    this.slots[this.active].el.pause()
  }

  seek(seconds: number): void {
    this.cancelFade()
    this.slots[this.active].el.currentTime = seconds
  }

  setVolume(volume: number, muted: boolean): void {
    this.volume = volume
    this.muted = muted
    for (const s of this.slots) this.applyGain(s, s.fade)
  }

  refreshNormalization(): void {
    for (const s of this.slots) {
      if (s.track) s.trackGain = this.gainFor(s.track)
      this.applyGain(s, s.fade)
    }
  }

  replaceTrack(track: Track): void {
    for (const s of this.slots) if (s.track?.id === track.id) s.track = track
  }

  stop(): void {
    this.deferredNext = undefined
    this.cancelFade()
    for (const s of this.slots) {
      s.el.pause()
      s.el.removeAttribute("src")
      s.el.load()
      s.track = null
    }
  }

  private makeSlot(index: number): Slot {
    const el = new Audio()
    el.preload = "metadata"
    const slot: Slot = { el, gain: null, track: null, trackGain: 1, fade: 1 }

    el.addEventListener("timeupdate", () => {
      if (this.slots[this.active] !== slot) return
      this.events.onTime(el.currentTime)
      this.maybeStartCrossfade()
    })
    el.addEventListener("loadedmetadata", () => {
      if (this.slots[this.active] === slot && Number.isFinite(el.duration)) this.events.onDuration(el.duration)
    })
    el.addEventListener("durationchange", () => {
      if (this.slots[this.active] === slot && Number.isFinite(el.duration)) this.events.onDuration(el.duration)
    })
    el.addEventListener("play", () => {
      if (this.slots[this.active] === slot) this.events.onPlay()
    })
    el.addEventListener("playing", () => {
      if (this.slots[this.active] === slot) this.events.onPlay()
    })
    el.addEventListener("pause", () => {
      if (this.slots[this.active] === slot && !this.fading) this.events.onPause()
    })
    el.addEventListener("ended", () => {
      if (this.handedOff.has(el)) {
        this.handedOff.delete(el)
        return
      }
      if (this.slots[this.active] !== slot) return
      const next = this.slots[1 - this.active]
      if (next.track && next.el.src) {
        this.active = 1 - this.active
        this.applyGain(next, 1)
        next.el.play().catch(() => this.events.onError())
        this.events.onAdvanced(next.track)
        return
      }
      this.events.onEnded()
    })
    el.addEventListener("error", () => {
      if (this.slots[this.active] === slot && slot.track) this.events.onError()
    })
    void index
    return slot
  }

  private startSlot(slot: Slot, opts: { autoplay: boolean; startAt?: number }) {
    if (opts.startAt && opts.startAt > 0) {
      const at = opts.startAt
      const seek = () => {
        slot.el.currentTime = at
        slot.el.removeEventListener("loadedmetadata", seek)
      }
      if (slot.el.readyState >= 1) slot.el.currentTime = at
      else slot.el.addEventListener("loadedmetadata", seek)
    }
    if (opts.autoplay) {
      this.resumeContext()
      slot.el.play().catch((e: unknown) => {
        // Another load interrupting this one isn't a failure to play.
        if ((e as Error)?.name !== "AbortError") this.events.onError()
      })
    }
  }

  private silence(slot: Slot) {
    slot.el.pause()
    slot.fade = 0
  }

  private gainFor(track: Track): number {
    if (!this.normalize || track.loudness == null) return 1
    const db = Math.min(TARGET_LUFS - track.loudness, MAX_BOOST_DB)
    return Math.max(MIN_GAIN, Math.pow(10, db / 20))
  }

  private applyGain(slot: Slot, fade: number) {
    slot.fade = fade
    this.ensureContext(slot)
    if (slot.gain) {
      slot.el.volume = 1
      slot.el.muted = this.muted
      slot.gain.gain.value = this.volume * slot.trackGain * fade
    } else {
      slot.el.muted = this.muted
      slot.el.volume = Math.min(1, this.volume * slot.trackGain * fade)
    }
  }

  private ensureContext(slot: Slot) {
    if (slot.gain || typeof window === "undefined") return
    if (!this.ctx && !this.ctxTried) {
      this.ctxTried = true
      try {
        const Ctor = window.AudioContext ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
        if (Ctor) this.ctx = new Ctor()
      } catch {
        this.ctx = null
      }
    }
    if (!this.ctx) return
    try {
      const src = this.ctx.createMediaElementSource(slot.el)
      const gain = this.ctx.createGain()
      src.connect(gain)
      gain.connect(this.ctx.destination)
      slot.gain = gain
    } catch {}
  }

  private resumeContext() {
    if (this.ctx && this.ctx.state === "suspended") this.ctx.resume().catch(() => {})
  }

  private maybeStartCrossfade() {
    if (this.fading || this.crossfadeSeconds <= 0) return
    const cur = this.slots[this.active]
    const next = this.slots[1 - this.active]
    if (!next.track || !next.el.src || cur.el.paused) return
    const d = cur.el.duration
    if (!Number.isFinite(d) || d <= 0) return
    const remaining = d - cur.el.currentTime
    if (d < this.crossfadeSeconds * 2 || remaining > this.crossfadeSeconds) return

    this.fading = true
    const from = cur
    const to = next
    this.handedOff.add(from.el)
    this.active = 1 - this.active
    this.applyGain(to, 0)
    this.resumeContext()
    to.el.play().catch(() => this.events.onError())
    this.events.onAdvanced(to.track!)

    const durationMs = this.crossfadeSeconds * 1000
    const started = performance.now()
    const step = () => {
      const t = Math.min(1, (performance.now() - started) / durationMs)
      this.applyGain(from, Math.cos((t * Math.PI) / 2))
      this.applyGain(to, Math.sin((t * Math.PI) / 2))
      if (t < 1) {
        this.fadeRaf = requestAnimationFrame(step)
      } else {
        this.fadeRaf = null
        this.fading = false
        from.el.pause()
        from.track = null
        from.el.removeAttribute("src")
        this.applyGain(from, 1)
        this.flushDeferredNext()
      }
    }
    this.fadeRaf = requestAnimationFrame(step)
  }

  private flushDeferredNext() {
    if (this.deferredNext === undefined) return
    const track = this.deferredNext
    this.deferredNext = undefined
    this.prepareNext(track)
  }

  private cancelFade() {
    if (!this.fading) return
    if (this.fadeRaf !== null) cancelAnimationFrame(this.fadeRaf)
    this.fadeRaf = null
    this.fading = false
    const from = this.slots[1 - this.active]
    from.el.pause()
    from.track = null
    from.el.removeAttribute("src")
    this.applyGain(from, 1)
    this.applyGain(this.slots[this.active], 1)
    this.flushDeferredNext()
  }
}
