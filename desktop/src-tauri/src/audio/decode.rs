use std::sync::Arc;
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::thread;
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

pub struct SlotShared {
    pub id: u64,
    pub cancel: Arc<AtomicBool>,
    pub finished: AtomicBool,
    pub total_frames: AtomicU64,
    pub played: AtomicU64,
}

impl SlotShared {
    pub fn new(id: u64) -> Arc<Self> {
        Arc::new(SlotShared {
            id,
            cancel: Arc::new(AtomicBool::new(false)),
            finished: AtomicBool::new(false),
            total_frames: AtomicU64::new(0),
            played: AtomicU64::new(0),
        })
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

pub fn spawn(mut job: DecodeJob) {
    let _ = thread::Builder::new()
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
        });
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
            Some(resampler) => {
                let producer = &mut job.producer;
                let cancel = &job.slot.cancel;
                resampler.process(input, &mut |out| push(producer, out, cancel))?;
            }
            None => push(&mut job.producer, input, &job.slot.cancel)?,
        }
    }

    if let Some(resampler) = resample.as_mut() {
        let producer = &mut job.producer;
        let cancel = &job.slot.cancel;
        resampler.finish(&mut |out| push(producer, out, cancel))?;
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

fn push(
    producer: &mut Producer<f32>,
    mut data: &[f32],
    cancel: &AtomicBool,
) -> Result<(), Failure> {
    while !data.is_empty() {
        if cancel.load(Ordering::Relaxed) || producer.is_abandoned() {
            return Err(Failure::Cancelled);
        }
        let free = producer.slots() & !1;
        if free == 0 {
            thread::sleep(Duration::from_millis(15));
            continue;
        }
        let n = free.min(data.len());
        if let Ok(chunk) = producer.write_chunk_uninit(n) {
            chunk.fill_from_iter(data[..n].iter().copied());
            data = &data[n..];
        }
    }
    Ok(())
}

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
