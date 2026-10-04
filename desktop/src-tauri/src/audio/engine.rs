use std::sync::atomic::Ordering;
use std::sync::{Arc, Mutex, MutexGuard};
use std::time::{Duration, Instant};

use crossbeam_channel::{Receiver, RecvTimeoutError, Sender};
use rtrb::RingBuffer;
use tauri::AppHandle;
use ureq::Agent;

use super::decode::{self, DecodeJob, SlotShared};
use super::output::{self, MixSlot, Mixer, Output};
use super::{Emitter, Event, Info, Msg, Snapshot};
use crate::media;

const BUFFER_SECONDS: u32 = 2;
const TICK: Duration = Duration::from_millis(250);
const IDLE_TICK: Duration = Duration::from_secs(1);
const RELEASE_AFTER: Duration = Duration::from_secs(30);
const MEDIA_POSITION_EVERY: Duration = Duration::from_secs(10);
const FALLBACK_RATE: u32 = 48_000;
const DRAIN: Duration = Duration::from_millis(500);
const PREVIEW_BIT: u64 = 1 << 63;

struct Track {
    id: u64,
    url: String,
    gain: f32,
    start: f64,
    shared: Arc<SlotShared>,
    duration_sent: bool,
}

pub struct Engine {
    rx: Receiver<Msg>,
    tx: Sender<Msg>,
    events: Emitter,
    app: AppHandle,
    agent: Agent,
    mixer: Arc<Mutex<Mixer>>,
    output: Option<Output>,
    rate: u32,
    current: Option<Track>,
    next: Option<Track>,
    playing: bool,
    volume: f32,
    muted: bool,
    crossfade: f64,
    idle_since: Option<Instant>,
    last_tick: Instant,
    last_media_position: Instant,
    quiet_at: Option<Instant>,
    preview: Option<Arc<SlotShared>>,
    previews: u64,
}

impl Engine {
    pub fn new(rx: Receiver<Msg>, tx: Sender<Msg>, events: Emitter, app: AppHandle) -> Self {
        let mixer = Arc::new(Mutex::new(Mixer::new(tx.clone())));
        Engine {
            rx,
            tx,
            events,
            app,
            agent: super::http::agent(),
            mixer,
            output: None,
            rate: FALLBACK_RATE,
            current: None,
            next: None,
            playing: false,
            volume: 1.0,
            muted: false,
            crossfade: 0.0,
            idle_since: None,
            last_tick: Instant::now(),
            last_media_position: Instant::now(),
            quiet_at: None,
            preview: None,
            previews: 0,
        }
    }

    pub fn run(mut self) {
        loop {
            let mut timeout = if self.playing { TICK } else { IDLE_TICK };
            if let Some(at) = self.quiet_at {
                timeout = timeout.min(at.saturating_duration_since(Instant::now()));
            }
            match self.rx.recv_timeout(timeout) {
                Ok(msg) => self.handle(msg),
                Err(RecvTimeoutError::Timeout) => {}
                Err(RecvTimeoutError::Disconnected) => return,
            }
            if self.playing && self.last_tick.elapsed() >= TICK {
                self.tick();
            }
            self.pause_output_when_drained();
            self.release_when_idle();
        }
    }

    fn mixer(&self) -> MutexGuard<'_, Mixer> {
        self.mixer.lock().unwrap_or_else(|e| e.into_inner())
    }

    fn emit(&self, event: Event) {
        if let Ok(sink) = self.events.lock() {
            sink.send(event);
        }
    }

    fn handle(&mut self, msg: Msg) {
        match msg {
            Msg::Load {
                id,
                url,
                start,
                autoplay,
                gain,
            } => self.load(id, url, start, autoplay, gain),
            Msg::Prepare { next } => self.prepare(next),
            Msg::Play => self.play(),
            Msg::Pause => self.pause(),
            Msg::Seek(position) => self.seek(position),
            Msg::Volume { volume, muted } => {
                self.volume = volume.clamp(0.0, 1.0);
                self.muted = muted;
                let level = if muted { 0.0 } else { self.volume };
                self.mixer().volume = level;
            }
            Msg::Gains { current, next } => {
                if let Some(track) = self.current.as_mut() {
                    track.gain = current;
                }
                if let Some(track) = self.next.as_mut() {
                    track.gain = next;
                }
                let mut mixer = self.mixer();
                if let Some(slot) = mixer.active.as_mut() {
                    slot.gain = current;
                }
                if let Some(slot) = mixer.next.as_mut() {
                    slot.gain = next;
                }
            }
            Msg::Crossfade(seconds) => {
                self.crossfade = seconds.max(0.0);
                let frames = (self.crossfade * f64::from(self.rate)) as u64;
                self.mixer().crossfade_frames = frames;
            }
            Msg::Stop => self.stop(),
            Msg::Info(reply) => {
                let info = self.output.as_ref().map(|o| Info {
                    device: o.device.clone(),
                    sample_rate: o.rate,
                    channels: o.channels,
                    format: o.format.clone(),
                    playing: self.playing,
                });
                let _ = reply.send(info);
            }
            Msg::Snapshot(reply) => {
                let rate = f64::from(self.rate);
                let duration = self.current.as_ref().and_then(|track| {
                    let total = track.shared.total_frames.load(Ordering::Relaxed);
                    (total > 0).then(|| track.start + total as f64 / rate)
                });
                let _ = reply.send(Snapshot {
                    current: self.current.as_ref().map(|t| t.id),
                    next: self.next.as_ref().map(|t| t.id),
                    position: self.position(),
                    duration,
                    playing: self.playing,
                });
            }
            Msg::Media(action, position) => self.emit(Event::Media { action, position }),
            Msg::PreviewPlay { url, start, until } => self.preview_play(url, start, until),
            Msg::PreviewStop => {
                if let Some(shared) = self.preview.take() {
                    shared.cancel.store(true, Ordering::Relaxed);
                    self.mixer().fade_out_preview();
                    self.quiet_output();
                }
            }
            Msg::PreviewEnded(id) => {
                if self.preview.as_ref().is_some_and(|p| p.id == id) {
                    self.preview = None;
                    self.quiet_output();
                    self.emit(Event::Preview {
                        state: "ended".into(),
                    });
                }
            }
            Msg::Advanced(id) => self.advanced(id),
            Msg::Ended(id) => self.ended(id),
            Msg::Failed {
                id,
                message,
                expired,
            } => self.failed(id, message, expired),
            Msg::DeviceError(message) => self.device_lost(message),
        }
    }

    fn start_track(&self, id: u64, url: String, start: f64, gain: f32) -> (Track, MixSlot) {
        let shared = SlotShared::new(id);
        let capacity = (self.rate * BUFFER_SECONDS * 2) as usize;
        let (producer, consumer) = RingBuffer::<f32>::new(capacity);
        decode::spawn(DecodeJob {
            url: url.clone(),
            start,
            out_rate: self.rate,
            producer,
            slot: shared.clone(),
            notify: self.tx.clone(),
            agent: self.agent.clone(),
        });
        let track = Track {
            id,
            url,
            gain,
            start,
            shared: shared.clone(),
            duration_sent: false,
        };
        let slot = MixSlot::new(shared, consumer, gain);
        (track, slot)
    }

    fn cancel(track: &Option<Track>) {
        if let Some(track) = track {
            track.shared.cancel.store(true, Ordering::Relaxed);
        }
    }

    fn load(&mut self, id: u64, url: String, start: f64, autoplay: bool, gain: f32) {
        if autoplay {
            self.ensure_output();
        }
        let promote = start <= 0.0
            && self
                .next
                .as_ref()
                .is_some_and(|n| n.id == id && n.url == url);
        if promote {
            Self::cancel(&self.current);
            let mut track = self.next.take();
            if let Some(t) = track.as_mut() {
                t.gain = gain;
            }
            let mut mixer = self.mixer();
            mixer.clear_fade();
            mixer.active = mixer.next.take();
            if let Some(slot) = mixer.active.as_mut() {
                slot.gain = gain;
            }
            drop(mixer);
            self.current = track;
        } else {
            Self::cancel(&self.current);
            Self::cancel(&self.next);
            self.next = None;
            let (track, slot) = self.start_track(id, url, start, gain);
            let mut mixer = self.mixer();
            mixer.clear_fade();
            mixer.next = None;
            mixer.active = Some(slot);
            drop(mixer);
            self.current = Some(track);
        }
        if autoplay {
            self.play();
        } else {
            self.mixer().paused = true;
            self.emit_time();
        }
    }

    fn prepare(&mut self, next: Option<(u64, String, f32)>) {
        if let (Some((id, url, gain)), Some(existing)) = (next.as_ref(), self.next.as_mut())
            && existing.id == *id
            && existing.url == *url
        {
            existing.gain = *gain;
            if let Some(slot) = self
                .mixer
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .next
                .as_mut()
            {
                slot.gain = *gain;
            }
            return;
        }
        Self::cancel(&self.next);
        self.next = None;
        self.mixer().next = None;
        if let Some((id, url, gain)) = next {
            let (track, slot) = self.start_track(id, url, 0.0, gain);
            self.mixer().next = Some(slot);
            self.next = Some(track);
        }
    }

    fn ensure_output(&mut self) -> bool {
        if self.output.is_some() {
            return true;
        }
        match output::open(self.mixer.clone(), self.tx.clone()) {
            Ok(output) => {
                let changed = output.rate != self.rate;
                self.rate = output.rate;
                self.mixer().crossfade_frames = (self.crossfade * f64::from(self.rate)) as u64;
                self.output = Some(output);
                if changed {
                    self.restart_decoders();
                }
                true
            }
            Err(message) => {
                if let Some(track) = self.current.as_ref() {
                    self.emit(Event::Error {
                        id: track.id,
                        message,
                        expired: false,
                    });
                }
                false
            }
        }
    }

    fn position(&self) -> f64 {
        match self.current.as_ref() {
            Some(track) => {
                track.start
                    + track.shared.played.load(Ordering::Relaxed) as f64 / f64::from(self.rate)
            }
            None => 0.0,
        }
    }

    fn restart_decoders(&mut self) {
        let position = self.position();
        if let Some(track) = self.current.take() {
            track.shared.cancel.store(true, Ordering::Relaxed);
            let (fresh, slot) = self.start_track(track.id, track.url, position, track.gain);
            self.mixer().active = Some(slot);
            self.current = Some(fresh);
        }
        if let Some(track) = self.next.take() {
            track.shared.cancel.store(true, Ordering::Relaxed);
            let (fresh, slot) = self.start_track(track.id, track.url, 0.0, track.gain);
            self.mixer().next = Some(slot);
            self.next = Some(fresh);
        }
        self.mixer().clear_fade();
    }

    fn play(&mut self) {
        let Some(id) = self.current.as_ref().map(|t| t.id) else {
            return;
        };
        if self.mixer().active.is_none() {
            let position = self.position();
            if let Some(track) = self.current.take() {
                let (fresh, slot) = self.start_track(track.id, track.url, position, track.gain);
                self.mixer().active = Some(slot);
                self.current = Some(fresh);
            }
        }
        if !self.ensure_output() {
            return;
        }
        self.mixer().paused = false;
        self.quiet_at = None;
        if let Some(output) = self.output.as_ref()
            && let Err(message) = output.play()
        {
            self.emit(Event::Error {
                id,
                message,
                expired: false,
            });
            return;
        }
        self.playing = true;
        self.idle_since = None;
        self.emit(Event::Play { id });
        self.emit_time();
        self.update_media_playback();
    }

    fn pause(&mut self) {
        self.mixer().paused = true;
        let was_playing = self.playing;
        self.playing = false;
        self.quiet_output();
        if was_playing && let Some(track) = self.current.as_ref() {
            self.emit(Event::Pause { id: track.id });
        }
        self.emit_time();
        self.update_media_playback();
    }

    fn seek(&mut self, position: f64) {
        let Some(track) = self.current.take() else {
            return;
        };
        track.shared.cancel.store(true, Ordering::Relaxed);
        let target = position.max(0.0);
        let (fresh, slot) = self.start_track(track.id, track.url, target, track.gain);
        let mut mixer = self.mixer();
        mixer.clear_fade();
        mixer.active = Some(slot);
        drop(mixer);
        self.current = Some(fresh);
        self.emit_time();
        self.update_media_playback();
    }

    fn stop(&mut self) {
        Self::cancel(&self.current);
        Self::cancel(&self.next);
        self.current = None;
        self.next = None;
        let mut mixer = self.mixer();
        mixer.active = None;
        mixer.next = None;
        mixer.clear_fade();
        mixer.paused = true;
        drop(mixer);
        self.playing = false;
        self.output = None;
        self.quiet_at = None;
        self.idle_since = None;
        media::clear(&self.app);
    }

    fn advanced(&mut self, id: u64) {
        if !self.next.as_ref().is_some_and(|n| n.id == id) {
            return;
        }
        self.current = self.next.take();
        self.emit(Event::Advanced { id });
        self.emit_time();
    }

    fn ended(&mut self, id: u64) {
        if !self.current.as_ref().is_some_and(|t| t.id == id) {
            return;
        }
        self.playing = false;
        self.mixer().paused = true;
        self.quiet_output();
        self.emit(Event::Ended { id });
        self.update_media_playback();
    }

    fn failed(&mut self, id: u64, message: String, expired: bool) {
        if id & PREVIEW_BIT != 0 {
            if self.preview.as_ref().is_some_and(|p| p.id == id) {
                self.cancel_preview();
                self.quiet_output();
                self.emit(Event::Preview {
                    state: "error".into(),
                });
            }
            return;
        }
        if self.next.as_ref().is_some_and(|n| n.id == id) {
            self.next = None;
            self.mixer().next = None;
            return;
        }
        if !self.current.as_ref().is_some_and(|t| t.id == id) {
            return;
        }
        if self.playing {
            self.mixer().paused = true;
            self.playing = false;
            self.quiet_output();
        }
        self.mixer().active = None;
        self.emit(Event::Error {
            id,
            message,
            expired,
        });
        self.update_media_playback();
    }

    fn device_lost(&mut self, message: String) {
        self.output = None;
        self.quiet_at = None;
        if !self.playing {
            return;
        }
        if !self.ensure_output() {
            self.playing = false;
            if let Some(track) = self.current.as_ref() {
                self.emit(Event::Error {
                    id: track.id,
                    message,
                    expired: false,
                });
            }
            return;
        }
        if let Some(output) = self.output.as_ref() {
            let _ = output.play();
        }
    }

    fn tick(&mut self) {
        self.last_tick = Instant::now();
        self.emit_time();
        let rate = f64::from(self.rate);
        let mut duration = None;
        if let Some(track) = self.current.as_mut()
            && !track.duration_sent
        {
            let total = track.shared.total_frames.load(Ordering::Relaxed);
            if total > 0 {
                track.duration_sent = true;
                duration = Some((track.id, track.start + total as f64 / rate));
            }
        }
        if let Some((id, duration)) = duration {
            self.emit(Event::Duration { id, duration });
        }
        if self.last_media_position.elapsed() >= MEDIA_POSITION_EVERY {
            self.update_media_playback();
        }
    }

    fn emit_time(&self) {
        if let Some(track) = self.current.as_ref() {
            self.emit(Event::Time {
                id: track.id,
                position: self.position(),
            });
        }
    }

    fn update_media_playback(&mut self) {
        self.last_media_position = Instant::now();
        let state = if self.current.is_none() {
            None
        } else {
            Some((self.playing, self.position()))
        };
        media::playback(&self.app, state);
    }

    fn preview_play(&mut self, url: String, start: f64, until: f64) {
        self.cancel_preview();
        if !self.ensure_output() {
            self.emit(Event::Preview {
                state: "error".into(),
            });
            return;
        }
        self.previews += 1;
        let shared = SlotShared::new(PREVIEW_BIT | self.previews);
        let capacity = (self.rate * BUFFER_SECONDS * 2) as usize;
        let (producer, consumer) = RingBuffer::<f32>::new(capacity);
        decode::spawn(DecodeJob {
            url,
            start: start.max(0.0),
            out_rate: self.rate,
            producer,
            slot: shared.clone(),
            notify: self.tx.clone(),
            agent: self.agent.clone(),
        });
        let frames = ((until - start).max(0.0) * f64::from(self.rate)) as u64;
        self.mixer()
            .start_preview(MixSlot::new(shared.clone(), consumer, 1.0), frames);
        self.preview = Some(shared);
        self.quiet_at = None;
        if let Some(output) = self.output.as_ref() {
            let _ = output.play();
        }
        self.idle_since = None;
        self.emit(Event::Preview {
            state: "playing".into(),
        });
    }

    fn cancel_preview(&mut self) {
        if let Some(shared) = self.preview.take() {
            shared.cancel.store(true, Ordering::Relaxed);
        }
    }

    fn quiet_output(&mut self) {
        if self.playing || self.preview.is_some() {
            return;
        }
        self.idle_since = Some(Instant::now());
        if self.output.is_some() {
            self.quiet_at = Some(Instant::now() + DRAIN);
        }
    }

    fn pause_output_when_drained(&mut self) {
        if self.quiet_at.is_none_or(|at| Instant::now() < at) {
            return;
        }
        self.quiet_at = None;
        if self.playing || self.preview.is_some() {
            return;
        }
        if let Some(output) = self.output.as_ref() {
            output.pause();
        }
    }

    fn release_when_idle(&mut self) {
        if self.playing || self.preview.is_some() || self.output.is_none() {
            return;
        }
        if self
            .idle_since
            .is_some_and(|since| since.elapsed() >= RELEASE_AFTER)
        {
            self.output = None;
            self.quiet_at = None;
            self.idle_since = None;
        }
    }
}
