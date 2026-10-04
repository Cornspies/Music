use std::f32::consts::FRAC_PI_2;
use std::sync::atomic::Ordering;
use std::sync::{Arc, Mutex};

use cpal::traits::{DeviceTrait, HostTrait, StreamTrait};
use cpal::{
    BufferSize, ErrorKind, FromSample, SampleFormat, SizedSample, Stream, StreamConfig,
    SupportedBufferSize,
};
use crossbeam_channel::Sender;
use rtrb::Consumer;

use super::Msg;
use super::decode::SlotShared;

const VOLUME_RAMP_FRAMES: f32 = 512.0;
const TRANSPORT_RAMP_FRAMES: f32 = 960.0;
const PREVIEW_RAMP_FRAMES: u64 = 960;
const PREROLL_FRAMES: usize = 8192;

pub struct MixSlot {
    pub shared: Arc<SlotShared>,
    pub consumer: Consumer<f32>,
    pub gain: f32,
    primed: bool,
}

impl MixSlot {
    pub fn new(shared: Arc<SlotShared>, consumer: Consumer<f32>, gain: f32) -> Self {
        MixSlot {
            shared,
            consumer,
            gain,
            primed: false,
        }
    }

    fn ready(&mut self, need: usize) -> bool {
        let buffered = self.consumer.slots() / 2;
        let finished = self.shared.finished.load(Ordering::Acquire);
        if buffered == 0 && !finished {
            self.primed = false;
        }
        if !self.primed && (buffered >= need || finished) {
            self.primed = true;
        }
        self.primed
    }
}

struct Voice {
    slot: MixSlot,
    left: u64,
    played: u64,
}

impl Voice {
    fn fade_out(&mut self) {
        self.left = self.left.min(PREVIEW_RAMP_FRAMES);
    }

    fn mix_into(&mut self, out: &mut [f32]) -> bool {
        let want = ((out.len() / 2) as u64).min(self.left) as usize;
        let need = (PREROLL_FRAMES as u64).min(self.left) as usize;
        let available = if self.slot.ready(need) {
            (self.slot.consumer.slots() / 2).min(want)
        } else {
            0
        };
        if available > 0
            && let Ok(chunk) = self.slot.consumer.read_chunk(available * 2)
        {
            let (first, second) = chunk.as_slices();
            for (k, (dst, src)) in stereo_frames_mut(&mut out[..available * 2])
                .iter_mut()
                .zip(stereo_frames(first).iter().chain(stereo_frames(second)))
                .enumerate()
            {
                let k = k as u64;
                let level = edge_gain(self.played + k) * edge_gain(self.left - k) * self.slot.gain;
                dst[0] += src[0] * level;
                dst[1] += src[1] * level;
            }
            chunk.commit_all();
            self.left -= available as u64;
            self.played += available as u64;
        }
        let shared = &self.slot.shared;
        let drained = available == 0
            && (shared.finished.load(Ordering::Acquire) || shared.cancel.load(Ordering::Relaxed));
        self.left == 0 || drained
    }
}

pub struct Mixer {
    pub active: Option<MixSlot>,
    pub next: Option<MixSlot>,
    pub outgoing: Option<MixSlot>,
    preview: Option<Voice>,
    preview_tail: Option<Voice>,
    fade_pos: u64,
    fade_len: u64,
    pub crossfade_frames: u64,
    pub paused: bool,
    transport: f32,
    pub volume: f32,
    applied_volume: f32,
    notify: Sender<Msg>,
}

impl Mixer {
    pub fn new(notify: Sender<Msg>) -> Self {
        Mixer {
            active: None,
            next: None,
            outgoing: None,
            fade_pos: 0,
            fade_len: 0,
            crossfade_frames: 0,
            preview: None,
            preview_tail: None,
            paused: true,
            transport: 0.0,
            volume: 1.0,
            applied_volume: 1.0,
            notify,
        }
    }

    pub fn clear_fade(&mut self) {
        self.outgoing = None;
        self.fade_pos = 0;
        self.fade_len = 0;
    }

    fn maybe_start_crossfade(&mut self) {
        if self.outgoing.is_some() || self.crossfade_frames == 0 || self.next.is_none() {
            return;
        }
        let Some(active) = self.active.as_ref() else {
            return;
        };
        let total = active.shared.total_frames.load(Ordering::Relaxed);
        if total == 0 {
            return;
        }
        let played = active.shared.played.load(Ordering::Relaxed);
        let remaining = total.saturating_sub(played);
        if remaining > self.crossfade_frames || remaining < self.crossfade_frames / 4 {
            return;
        }
        let Some(next) = self.next.take() else {
            return;
        };
        let id = next.shared.id;
        self.outgoing = self.active.replace(next);
        self.fade_pos = 0;
        self.fade_len = remaining.max(1);
        let _ = self.notify.try_send(Msg::Advanced(id));
    }

    pub fn start_preview(&mut self, slot: MixSlot, frames: u64) {
        self.preview_tail = self.preview.take().map(|mut old| {
            old.fade_out();
            old
        });
        self.preview = Some(Voice {
            slot,
            left: frames,
            played: 0,
        });
    }

    pub fn fade_out_preview(&mut self) {
        if let Some(voice) = self.preview.as_mut() {
            voice.fade_out();
        }
    }

    pub fn render(&mut self, out: &mut [f32]) {
        out.fill(0.0);
        let previewing = self.preview.is_some() || self.preview_tail.is_some();
        let running = !self.paused || self.transport > 0.0;
        if !running && !previewing {
            self.applied_volume = self.volume;
            return;
        }
        if running {
            let frames = if self.paused {
                ((self.transport * TRANSPORT_RAMP_FRAMES).ceil() as usize).min(out.len() / 2)
            } else {
                out.len() / 2
            };
            let main = &mut out[..frames * 2];
            self.render_main(main);
            self.apply_transport(main);
        }
        if previewing {
            self.render_preview(out);
        }
        self.apply_volume(out);
    }

    fn render_preview(&mut self, out: &mut [f32]) {
        if let Some(tail) = self.preview_tail.as_mut()
            && tail.mix_into(out)
        {
            self.preview_tail = None;
        }
        if let Some(voice) = self.preview.as_mut()
            && voice.mix_into(out)
        {
            let id = voice.slot.shared.id;
            self.preview = None;
            let _ = self.notify.try_send(Msg::PreviewEnded(id));
        }
    }

    fn render_main(&mut self, out: &mut [f32]) {
        let frames = out.len() / 2;
        let mut done = 0;
        while done < frames {
            self.maybe_start_crossfade();
            let Some(active) = self.active.as_mut() else {
                break;
            };
            if !active.ready(PREROLL_FRAMES) {
                break;
            }
            let available = active.consumer.slots() / 2;
            if available == 0 {
                if active.shared.finished.load(Ordering::Acquire) {
                    let ended = active.shared.id;
                    self.clear_fade();
                    match self.next.take() {
                        Some(next) => {
                            let id = next.shared.id;
                            self.active = Some(next);
                            let _ = self.notify.try_send(Msg::Advanced(id));
                            continue;
                        }
                        None => {
                            self.active = None;
                            let _ = self.notify.try_send(Msg::Ended(ended));
                            break;
                        }
                    }
                }
                break;
            }
            let n = available.min(frames - done);
            let fading = self.outgoing.is_some() && self.fade_len > 0;
            let fade_in_start = self.fade_pos;
            let gain = active.gain;
            let target = &mut out[done * 2..(done + n) * 2];
            if let Ok(chunk) = active.consumer.read_chunk(n * 2) {
                let (first, second) = chunk.as_slices();
                for (i, (dst, src)) in stereo_frames_mut(target)
                    .iter_mut()
                    .zip(stereo_frames(first).iter().chain(stereo_frames(second)))
                    .enumerate()
                {
                    let level = if fading {
                        fade_gain(fade_in_start + i as u64, self.fade_len, true)
                    } else {
                        1.0
                    };
                    dst[0] = src[0] * gain * level;
                    dst[1] = src[1] * gain * level;
                }
                chunk.commit_all();
            }
            active.shared.played.fetch_add(n as u64, Ordering::Relaxed);
            if fading {
                self.mix_outgoing(target, fade_in_start, n);
                self.fade_pos += n as u64;
                if self.fade_pos >= self.fade_len {
                    self.clear_fade();
                }
            }
            done += n;
        }
    }

    fn apply_transport(&mut self, out: &mut [f32]) {
        let target = if self.paused { 0.0 } else { 1.0 };
        if self.transport == target {
            return;
        }
        let step = 1.0 / TRANSPORT_RAMP_FRAMES;
        let mut level = self.transport;
        for frame in stereo_frames_mut(out) {
            level = if level < target {
                (level + step).min(target)
            } else {
                (level - step).max(target)
            };
            frame[0] *= level;
            frame[1] *= level;
        }
        self.transport = level;
    }

    fn apply_volume(&mut self, out: &mut [f32]) {
        let target = self.volume;
        let step = 1.0 / VOLUME_RAMP_FRAMES;
        let mut level = self.applied_volume;
        for frame in stereo_frames_mut(out) {
            if level < target {
                level = (level + step).min(target);
            } else if level > target {
                level = (level - step).max(target);
            }
            frame[0] *= level;
            frame[1] *= level;
        }
        self.applied_volume = level;
    }

    fn mix_outgoing(&mut self, target: &mut [f32], fade_start: u64, n: usize) {
        let fade_len = self.fade_len;
        let Some(outgoing) = self.outgoing.as_mut() else {
            return;
        };
        let gain = outgoing.gain;
        let available = (outgoing.consumer.slots() / 2).min(n);
        if available > 0
            && let Ok(chunk) = outgoing.consumer.read_chunk(available * 2)
        {
            let (first, second) = chunk.as_slices();
            for (i, (dst, src)) in stereo_frames_mut(target)
                .iter_mut()
                .zip(stereo_frames(first).iter().chain(stereo_frames(second)))
                .enumerate()
            {
                let level = fade_gain(fade_start + i as u64, fade_len, false);
                dst[0] += src[0] * gain * level;
                dst[1] += src[1] * gain * level;
            }
            chunk.commit_all();
        }
        if available == 0 && outgoing.shared.finished.load(Ordering::Acquire) {
            self.outgoing = None;
        }
    }
}

fn stereo_frames(samples: &[f32]) -> &[[f32; 2]] {
    samples.as_chunks::<2>().0
}

fn stereo_frames_mut(samples: &mut [f32]) -> &mut [[f32; 2]] {
    samples.as_chunks_mut::<2>().0
}

fn edge_gain(frames: u64) -> f32 {
    (frames as f32 / PREVIEW_RAMP_FRAMES as f32).min(1.0)
}

fn fade_gain(pos: u64, len: u64, rising: bool) -> f32 {
    let t = (pos as f32 / len as f32).clamp(0.0, 1.0);
    if rising {
        (t * FRAC_PI_2).sin()
    } else {
        (t * FRAC_PI_2).cos()
    }
}

pub struct Output {
    stream: Stream,
    pub rate: u32,
    pub channels: u16,
    pub format: String,
    pub device: String,
}

impl Output {
    pub fn play(&self) -> Result<(), String> {
        self.stream.play().map_err(|e| e.to_string())
    }

    pub fn pause(&self) {
        let _ = self.stream.pause();
    }
}

pub fn open(mixer: Arc<Mutex<Mixer>>, notify: Sender<Msg>) -> Result<Output, String> {
    let host = cpal::default_host();
    let device = host
        .default_output_device()
        .ok_or_else(|| "no audio output device was found".to_string())?;
    let name = device
        .description()
        .map(|d| d.to_string())
        .unwrap_or_else(|_| "default".into());
    let supported = device
        .default_output_config()
        .map_err(|e| format!("the audio device can't play: {e}"))?;
    let rate = supported.sample_rate();
    let channels = supported.channels();
    let format = supported.sample_format();
    let preferred = match supported.buffer_size() {
        SupportedBufferSize::Range { min, max } => {
            Some(BufferSize::Fixed((rate / 20).clamp(*min, *max)))
        }
        _ => None,
    };
    let mut attempts = Vec::new();
    if let Some(size) = preferred {
        attempts.push(size);
    }
    attempts.push(BufferSize::Default);
    let mut last_error = String::new();
    for buffer_size in attempts {
        let config = StreamConfig {
            channels,
            sample_rate: rate,
            buffer_size,
        };
        let built = match format {
            SampleFormat::F32 => build::<f32>(&device, config, mixer.clone(), notify.clone()),
            SampleFormat::I16 => build::<i16>(&device, config, mixer.clone(), notify.clone()),
            SampleFormat::U16 => build::<u16>(&device, config, mixer.clone(), notify.clone()),
            SampleFormat::I32 => build::<i32>(&device, config, mixer.clone(), notify.clone()),
            SampleFormat::F64 => build::<f64>(&device, config, mixer.clone(), notify.clone()),
            other => Err(format!("the audio device wants {other} samples")),
        };
        match built {
            Ok(stream) => {
                return Ok(Output {
                    stream,
                    rate,
                    channels,
                    format: format.to_string(),
                    device: name,
                });
            }
            Err(err) => last_error = err,
        }
    }
    Err(last_error)
}

fn build<T>(
    device: &cpal::Device,
    config: StreamConfig,
    mixer: Arc<Mutex<Mixer>>,
    notify: Sender<Msg>,
) -> Result<Stream, String>
where
    T: SizedSample + FromSample<f32>,
{
    let channels = usize::from(config.channels).max(1);
    let mut scratch: Vec<f32> = Vec::with_capacity(32 * 1024);
    device
        .build_output_stream::<T, _, _>(
            config,
            move |data: &mut [T], _| {
                let frames = data.len() / channels;
                scratch.resize(frames * 2, 0.0);
                match mixer.try_lock() {
                    Ok(mut mixer) => mixer.render(&mut scratch),
                    Err(_) => scratch.fill(0.0),
                }
                for (frame, stereo) in data.chunks_exact_mut(channels).zip(stereo_frames(&scratch))
                {
                    let left = stereo[0].clamp(-1.0, 1.0);
                    let right = stereo[1].clamp(-1.0, 1.0);
                    if channels == 1 {
                        frame[0] = T::from_sample((left + right) * 0.5);
                        continue;
                    }
                    frame[0] = T::from_sample(left);
                    frame[1] = T::from_sample(right);
                    for sample in &mut frame[2..] {
                        *sample = T::EQUILIBRIUM;
                    }
                }
            },
            move |err: cpal::Error| {
                if matches!(
                    err.kind(),
                    ErrorKind::DeviceNotAvailable | ErrorKind::HostUnavailable
                ) {
                    let _ = notify.try_send(Msg::DeviceError(err.to_string()));
                }
            },
            None,
        )
        .map_err(|e| e.to_string())
}
