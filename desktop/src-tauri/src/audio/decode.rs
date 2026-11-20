use std::sync::Arc;
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::thread::{self, Thread};
use std::time::Duration;

use crossbeam_channel::Sender;
use rtrb::Producer;
use rubato::audioadapter_buffers::direct::InterleavedSlice;
use rubato::{Fft, FixedSync, Indexing, Resampler};
use symphonia::core::codecs::audio::AudioDecoderOptions;
use symphonia::core::errors::Error as SymphoniaError;
use symphonia::core::formats::probe::Hint;
use symphonia::core::formats::{FormatOptions, SeekMode, SeekTo, TrackType};
use symphonia::core::io::{MediaSourceStream, MediaSourceStreamOptions};
use symphonia::core::meta::MetadataOptions;
use symphonia::core::units::{Time, Timestamp};
use symphonia::default::{get_codecs, get_probe};
use ureq::Agent;

use super::Msg;
use super::http::{self, HttpSource};

const RESAMPLE_CHUNK: usize = 1024;
const REFILL_DIVISOR: usize = 4;
const SHORTEST_WAIT: Duration = Duration::from_millis(50);

pub struct SlotShared {
    pub id: u64,
    pub cancel: Arc<AtomicBool>,
    pub finished: AtomicBool,
    pub total_frames: AtomicU64,
    pub played: AtomicU64,
    held: AtomicBool,
}

impl SlotShared {
    pub fn new(id: u64, held: bool) -> Arc<Self> {
        Arc::new(SlotShared {
            id,
            cancel: Arc::new(AtomicBool::new(false)),
            finished: AtomicBool::new(false),
            total_frames: AtomicU64::new(0),
            played: AtomicU64::new(0),
            held: AtomicBool::new(held),
        })
    }
}

pub struct Decoder {
    pub shared: Arc<SlotShared>,
    thread: Option<Thread>,
}

impl Decoder {
    pub fn cancel(&self) {
        self.shared.cancel.store(true, Ordering::Relaxed);
        self.wake();
    }

    // A held decoder whose ring is full sleeps until it's released, so its
    // slot must not be played while it's held.
    pub fn hold(&self, held: bool) {
        self.shared.held.store(held, Ordering::Relaxed);
        if !held {
            self.wake();
        }
    }

    fn wake(&self) {
        if let Some(thread) = &self.thread {
            thread.unpark();
        }
    }
}

impl Drop for Decoder {
    // Nothing else could wake the thread afterwards.
    fn drop(&mut self) {
        self.hold(false);
    }
}

pub struct DecodeJob {
    pub url: String,
    pub start: f64,
    pub out_rate: u32,
    pub producer: Producer<f32>,
    pub slot: Arc<SlotShared>,
    pub notify: Sender<Msg>,
    pub agent: Agent,
}

const UNSUPPORTED: &str = "The desktop app can't play this format. The server is making a playable copy, so try again in a minute.";

enum Failure {
    Cancelled,
    Expired(String),
    Other(String),
}

impl From<std::io::Error> for Failure {
    fn from(err: std::io::Error) -> Self {
        if http::is_cancelled(&err) {
            Failure::Cancelled
        } else if http::is_expired(&err) {
            Failure::Expired(err.to_string())
        } else {
            Failure::Other(err.to_string())
        }
    }
}

impl From<SymphoniaError> for Failure {
    fn from(err: SymphoniaError) -> Self {
        match err {
            SymphoniaError::IoError(io) => io.into(),
            SymphoniaError::Unsupported(what) => {
                Failure::Other(format!("this audio format isn't supported ({what})"))
            }
            other => Failure::Other(other.to_string()),
        }
    }
}

pub fn spawn(mut job: DecodeJob) -> Decoder {
    let shared = job.slot.clone();
    let thread = thread::Builder::new()
        .name("decoder".into())
        .spawn(move || {
            let result = decode(&mut job);
            let slot = &job.slot;
            match result {
                Ok(()) => slot.finished.store(true, Ordering::Release),
                Err(Failure::Cancelled) => {}
                Err(Failure::Expired(message)) => {
                    let _ = job.notify.send(Msg::Failed {
                        id: slot.id,
                        message,
                        expired: true,
                    });
                }
                Err(Failure::Other(message)) => {
                    if !slot.cancel.load(Ordering::Relaxed) {
                        let _ = job.notify.send(Msg::Failed {
                            id: slot.id,
                            message,
                            expired: false,
                        });
                    }
                }
            }
        })
        .ok()
        .map(|handle| handle.thread().clone());
    Decoder { shared, thread }
}

fn decode(job: &mut DecodeJob) -> Result<(), Failure> {
    let mut hint = Hint::new();
    let stream = match crate::offline::local_audio(&job.url) {
        Some((file, content_type)) => {
            if let Some(content_type) = content_type {
                hint.mime_type(&content_type);
            }
            MediaSourceStream::new(Box::new(file), MediaSourceStreamOptions::default())
        }
        None => {
            let source =
                HttpSource::open(job.agent.clone(), job.url.clone(), job.slot.cancel.clone())?;
            if let Some(content_type) = source.content_type() {
                hint.mime_type(&content_type);
            }
            MediaSourceStream::new(Box::new(source), MediaSourceStreamOptions::default())
        }
    };
    let mut format = get_probe().probe(
        &hint,
        stream,
        FormatOptions::default(),
        MetadataOptions::default(),
    )?;
    let track = format
        .first_track_known_codec(TrackType::Audio)
        .ok_or_else(|| Failure::Other(UNSUPPORTED.into()))?;
    let track_id = track.id;
    let time_base = track.time_base;
    let num_frames = track.num_frames;
    let params = track
        .codec_params
        .as_ref()
        .and_then(|p| p.audio())
        .cloned()
        .ok_or_else(|| Failure::Other("the file has no playable audio".into()))?;
    let mut decoder = get_codecs()
        .make_audio_decoder(&params, &AudioDecoderOptions::default().gapless(true))
        .map_err(|err| match err {
            SymphoniaError::Unsupported(_) => Failure::Other(UNSUPPORTED.into()),
            other => other.into(),
        })?;

    let mut skip_frames = 0u64;
    if job.start > 0.0
        && let Some(time) = Time::try_from_secs_f64(job.start)
    {
        if let Ok(seeked) = format.seek(
            SeekMode::Accurate,
            SeekTo::Time {
                time,
                track_id: Some(track_id),
            },
        ) {
            let behind = seeked.required_ts.get() - seeked.actual_ts.get();
            if behind > 0
                && let (Some(tb), Some(rate)) = (time_base, params.sample_rate)
                && let Some(t) = tb.calc_time(Timestamp::new(behind))
            {
                skip_frames = (t.as_secs_f64() * f64::from(rate)).round() as u64;
            }
        }
        decoder.reset();
    }

    let mut feed = Feed::new(&mut job.producer, &job.slot, job.out_rate);
    let mut resample: Option<Resample> = None;
    let mut in_rate = 0u32;
    let mut interleaved: Vec<f32> = Vec::new();
    let mut stereo: Vec<f32> = Vec::new();

    loop {
        if job.slot.cancel.load(Ordering::Relaxed) {
            return Err(Failure::Cancelled);
        }
        let packet = match format.next_packet() {
            Ok(Some(packet)) => packet,
            Ok(None) => break,
            Err(SymphoniaError::IoError(err))
                if err.kind() == std::io::ErrorKind::UnexpectedEof =>
            {
                break;
            }
            Err(SymphoniaError::ResetRequired) => break,
            Err(err) => return Err(err.into()),
        };
        if packet.track_id != track_id {
            continue;
        }
        let decoded = match decoder.decode(&packet) {
            Ok(decoded) => decoded,
            Err(SymphoniaError::DecodeError(_)) => continue,
            Err(err) => return Err(err.into()),
        };
        let frames = decoded.frames();
        if frames == 0 {
            continue;
        }
        let channels = decoded.spec().channels().count().max(1);
        let rate = decoded.spec().rate();
        if in_rate == 0 {
            in_rate = rate;
            if rate != job.out_rate {
                resample = Resample::new(rate, job.out_rate);
                if resample.is_none() {
                    return Err(Failure::Other(format!(
                        "can't convert {rate} Hz audio for this output"
                    )));
                }
            }
            if let Some(total) = num_frames {
                let seconds = (total as f64 / f64::from(rate) - job.start).max(0.0);
                let out = (seconds * f64::from(job.out_rate)).round() as u64;
                job.slot.total_frames.store(out, Ordering::Relaxed);
            }
        }
        interleaved.resize(frames * channels, 0.0);
        decoded.copy_to_slice_interleaved::<f32, _>(&mut interleaved[..]);
        to_stereo(&interleaved, channels, &mut stereo);

        let mut input = &stereo[..];
        if skip_frames > 0 {
            let skip = (skip_frames as usize).min(input.len() / 2);
            input = &input[skip * 2..];
            skip_frames -= skip as u64;
            if input.is_empty() {
                continue;
            }
        }
        match resample.as_mut() {
            Some(resampler) => resampler.process(input, &mut |out| feed.push(out))?,
            None => feed.push(input)?,
        }
    }

    if let Some(resampler) = resample.as_mut() {
        resampler.finish(&mut |out| feed.push(out))?;
    }
    Ok(())
}

fn to_stereo(input: &[f32], channels: usize, out: &mut Vec<f32>) {
    out.clear();
    match channels {
        1 => {
            for &sample in input {
                out.push(sample);
                out.push(sample);
            }
        }
        2 => out.extend_from_slice(input),
        n => {
            for frame in input.chunks_exact(n) {
                let center = frame[2] * 0.707;
                out.push(frame[0] + center);
                out.push(frame[1] + center);
            }
        }
    }
}

struct Pacing {
    refill: usize,
    samples_per_second: f64,
    full: bool,
}

impl Pacing {
    fn new(capacity: usize, rate: u32) -> Self {
        Pacing {
            refill: (capacity / REFILL_DIVISOR).max(2) & !1,
            samples_per_second: f64::from(rate.max(1)) * 2.0,
            full: false,
        }
    }

    // `None` means write now. Once the ring is full it's left alone until
    // `refill` samples of it have been played, and the wait is the time that
    // takes, so the decoder comes back when there's that much room and not
    // every few milliseconds.
    fn wait(&mut self, free: usize) -> Option<Duration> {
        if free == 0 {
            self.full = true;
        } else if free >= self.refill {
            self.full = false;
        }
        if !self.full {
            return None;
        }
        let missing = (self.refill - free) as f64 / self.samples_per_second;
        Some(Duration::from_secs_f64(missing).max(SHORTEST_WAIT))
    }
}

struct Feed<'a> {
    producer: &'a mut Producer<f32>,
    slot: &'a SlotShared,
    pacing: Pacing,
}

impl<'a> Feed<'a> {
    fn new(producer: &'a mut Producer<f32>, slot: &'a SlotShared, rate: u32) -> Self {
        let pacing = Pacing::new(producer.buffer().capacity(), rate);
        Feed {
            producer,
            slot,
            pacing,
        }
    }

    fn push(&mut self, mut data: &[f32]) -> Result<(), Failure> {
        while !data.is_empty() {
            if self.slot.cancel.load(Ordering::Relaxed) || self.producer.is_abandoned() {
                return Err(Failure::Cancelled);
            }
            let free = self.producer.slots() & !1;
            if let Some(wait) = self.pacing.wait(free) {
                if self.slot.held.load(Ordering::Relaxed) {
                    thread::park();
                } else {
                    thread::park_timeout(wait);
                }
                continue;
            }
            let n = free.min(data.len());
            if let Ok(chunk) = self.producer.write_chunk_uninit(n) {
                chunk.fill_from_iter(data[..n].iter().copied());
                data = &data[n..];
            }
        }
        Ok(())
    }
}

const SOURCE_RATES: std::ops::RangeInclusive<u32> = 8_000..=384_000;

struct Resample {
    fft: Fft<f32>,
    pending: Vec<f32>,
    output: Vec<f32>,
    skip: usize,
    ratio: f64,
    input_frames: u64,
    output_frames: u64,
}

impl Resample {
    fn new(in_rate: u32, out_rate: u32) -> Option<Self> {
        // The rate comes from the file's header. The converter's buffers grow
        // with it, and a nonsense value asks for more memory than there is,
        // which ends the app.
        if !SOURCE_RATES.contains(&in_rate) {
            return None;
        }
        let fft = Fft::<f32>::new(
            in_rate as usize,
            out_rate as usize,
            RESAMPLE_CHUNK,
            2,
            FixedSync::Input,
        )
        .ok()?;
        let skip = fft.output_delay();
        let output = vec![0.0; fft.output_frames_max() * 2];
        Some(Resample {
            fft,
            pending: Vec::with_capacity(RESAMPLE_CHUNK * 4),
            output,
            skip,
            ratio: f64::from(out_rate) / f64::from(in_rate),
            input_frames: 0,
            output_frames: 0,
        })
    }

    fn process(
        &mut self,
        input: &[f32],
        sink: &mut dyn FnMut(&[f32]) -> Result<(), Failure>,
    ) -> Result<(), Failure> {
        self.pending.extend_from_slice(input);
        self.input_frames += (input.len() / 2) as u64;
        loop {
            let need = self.fft.input_frames_next();
            if self.pending.len() / 2 < need {
                return Ok(());
            }
            let written = self.run(need, None)?;
            self.pending.drain(..need * 2);
            self.emit(written, u64::MAX, sink)?;
        }
    }

    fn finish(
        &mut self,
        sink: &mut dyn FnMut(&[f32]) -> Result<(), Failure>,
    ) -> Result<(), Failure> {
        let expected = (self.input_frames as f64 * self.ratio).round() as u64;
        let mut rounds = 0;
        while self.output_frames < expected && rounds < 64 {
            rounds += 1;
            let need = self.fft.input_frames_next();
            let have = (self.pending.len() / 2).min(need);
            self.pending.resize(need * 2, 0.0);
            let written = self.run(need, Some(have))?;
            self.pending.clear();
            self.emit(written, expected, sink)?;
        }
        Ok(())
    }

    fn run(&mut self, frames: usize, partial: Option<usize>) -> Result<usize, Failure> {
        let capacity = self.output.len() / 2;
        let input = InterleavedSlice::new(&self.pending[..frames * 2], 2, frames)
            .map_err(|e| Failure::Other(e.to_string()))?;
        let mut output = InterleavedSlice::new_mut(&mut self.output[..], 2, capacity)
            .map_err(|e| Failure::Other(e.to_string()))?;
        let indexing = partial.map(|have| Indexing::new().partial_len(have));
        let (_, written) = self
            .fft
            .process_into_buffer(&input, &mut output, indexing.as_ref())
            .map_err(|e| Failure::Other(e.to_string()))?;
        Ok(written)
    }

    fn emit(
        &mut self,
        written: usize,
        limit: u64,
        sink: &mut dyn FnMut(&[f32]) -> Result<(), Failure>,
    ) -> Result<(), Failure> {
        let mut frames = &self.output[..written * 2];
        if self.skip > 0 {
            let skip = self.skip.min(written);
            frames = &frames[skip * 2..];
            self.skip -= skip;
        }
        let room = limit.saturating_sub(self.output_frames) as usize;
        let take = (frames.len() / 2).min(room);
        if take == 0 {
            return Ok(());
        }
        self.output_frames += take as u64;
        sink(&frames[..take * 2])
    }
}

#[cfg(test)]
mod tests {
    use std::sync::Arc;
    use std::thread::{self, JoinHandle};
    use std::time::{Duration, Instant};

    use rtrb::{Producer, RingBuffer};

    use super::Resample;
    use super::{Decoder, Feed, Pacing, SHORTEST_WAIT, SlotShared};

    #[test]
    fn resampler_refuses_rates_no_real_file_has() {
        assert!(Resample::new(44_100, 48_000).is_some());
        assert!(Resample::new(96_000, 44_100).is_some());
        assert!(Resample::new(1, 48_000).is_none());
        assert!(Resample::new(4_294_967_291, 48_000).is_none());
    }

    const RATE: u32 = 48_000;
    const RING: usize = 192_000;

    #[test]
    fn decoder_fills_the_ring_then_waits_until_a_quarter_has_played() {
        let mut pacing = Pacing::new(RING, RATE);
        assert_eq!(pacing.wait(RING), None);
        assert_eq!(pacing.wait(2), None);
        assert_eq!(pacing.wait(0), Some(Duration::from_millis(500)));
        assert_eq!(pacing.wait(24_000), Some(Duration::from_millis(250)));
        assert_eq!(pacing.wait(47_998), Some(SHORTEST_WAIT));
        assert_eq!(pacing.wait(48_000), None);
        assert_eq!(pacing.wait(2), None);
        assert_eq!(pacing.wait(0), Some(Duration::from_millis(500)));
    }

    #[test]
    fn playing_ring_stays_three_quarters_full_on_a_few_wake_ups_a_second() {
        const PERIOD: usize = 1024;
        const SECONDS: usize = 60;
        let rate = RATE as usize;
        let mut pacing = Pacing::new(RING, RATE);
        let mut buffered = 0;
        let mut lowest = RING;
        let mut wake_ups = 0;
        let mut now = 0;
        let mut callback = PERIOD;
        while now < SECONDS * rate {
            let Some(wait) = pacing.wait(RING - buffered) else {
                buffered = RING;
                continue;
            };
            wake_ups += 1;
            now += (wait.as_secs_f64() * f64::from(RATE)).ceil() as usize;
            while callback <= now {
                buffered -= PERIOD * 2;
                callback += PERIOD;
            }
            lowest = lowest.min(buffered);
        }
        let shortest = (SHORTEST_WAIT.as_secs_f64() * f64::from(RATE)) as usize * 2;
        assert!(lowest >= RING - RING / 4 - shortest - PERIOD * 2);
        assert!(wake_ups <= SECONDS * 4);
    }

    fn feeder(
        mut producer: Producer<f32>,
        shared: Arc<SlotShared>,
        samples: usize,
    ) -> (Decoder, JoinHandle<bool>) {
        let slot = shared.clone();
        let handle = thread::spawn(move || {
            let data: Vec<f32> = (0..samples).map(|i| i as f32).collect();
            let mut feed = Feed::new(&mut producer, &slot, RATE);
            data.chunks(6).all(|packet| feed.push(packet).is_ok())
        });
        let decoder = Decoder {
            shared,
            thread: Some(handle.thread().clone()),
        };
        (decoder, handle)
    }

    fn wait_for(mut done: impl FnMut() -> bool) {
        let deadline = Instant::now() + Duration::from_secs(10);
        while !done() {
            assert!(Instant::now() < deadline);
            thread::sleep(Duration::from_millis(1));
        }
    }

    #[test]
    fn samples_arrive_once_and_in_order() {
        let (producer, mut consumer) = RingBuffer::<f32>::new(64);
        let (_decoder, thread) = feeder(producer, SlotShared::new(1, false), 400);
        let mut heard = Vec::new();
        wait_for(|| {
            heard.extend(std::iter::from_fn(|| consumer.pop().ok()));
            heard.len() >= 400
        });
        assert!(thread.join().unwrap());
        assert!(heard.iter().copied().eq((0..400).map(|i| i as f32)));
    }

    #[test]
    fn held_decoder_sleeps_until_it_is_released() {
        let (producer, mut consumer) = RingBuffer::<f32>::new(64);
        let (decoder, thread) = feeder(producer, SlotShared::new(1, true), 128);
        wait_for(|| consumer.slots() == 64);
        while consumer.pop().is_ok() {}
        thread::sleep(SHORTEST_WAIT * 6);
        assert_eq!(consumer.slots(), 0);
        decoder.hold(false);
        wait_for(|| consumer.slots() == 64);
        assert!(thread.join().unwrap());
    }

    #[test]
    fn cancelling_or_dropping_its_handle_wakes_a_held_decoder() {
        let (producer, consumer) = RingBuffer::<f32>::new(64);
        let (decoder, thread) = feeder(producer, SlotShared::new(1, true), 128);
        wait_for(|| consumer.slots() == 64);
        decoder.cancel();
        wait_for(|| thread.is_finished());
        assert!(!thread.join().unwrap());

        let (producer, consumer) = RingBuffer::<f32>::new(64);
        let (decoder, thread) = feeder(producer, SlotShared::new(2, true), 128);
        wait_for(|| consumer.slots() == 64);
        drop(decoder);
        drop(consumer);
        wait_for(|| thread.is_finished());
        assert!(!thread.join().unwrap());
    }
}
