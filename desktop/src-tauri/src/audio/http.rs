use std::io::{self, Read, Seek, SeekFrom};
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, Ordering};
use std::thread;
use std::time::Duration;

use symphonia::core::io::MediaSource;
use ureq::Agent;
use ureq::http::Response;
use ureq::tls::{RootCerts, TlsConfig};
use ureq::unversioned::resolver::DefaultResolver;
use ureq::unversioned::transport::{
    Buffers, ConnectProxyConnector, ConnectionDetails, Connector, NextTimeout, RustlsConnector,
    TcpConnector, Transport, time,
};

const SKIP_FORWARD_LIMIT: u64 = 256 * 1024;
const MAX_ATTEMPTS: u32 = 4;
const STALL_TIMEOUT: Duration = Duration::from_secs(15);

pub fn agent() -> Agent {
    let config = Agent::config_builder()
        .http_status_as_error(false)
        .timeout_connect(Some(Duration::from_secs(10)))
        .timeout_recv_response(Some(Duration::from_secs(20)))
        .tls_config(
            TlsConfig::builder()
                .root_certs(RootCerts::PlatformVerifier)
                .build(),
        )
        .build();
    let connector =
        ().chain(ConnectProxyConnector::default())
            .chain(TcpConnector::default())
            .chain(RustlsConnector::default())
            .chain(StallTimeout);
    Agent::with_parts(config, connector, DefaultResolver::default())
}

#[derive(Debug)]
struct StallTimeout;

impl<In: Transport> Connector<In> for StallTimeout {
    type Out = Stalling<In>;

    fn connect(
        &self,
        _: &ConnectionDetails,
        chained: Option<In>,
    ) -> Result<Option<Self::Out>, ureq::Error> {
        Ok(chained.map(Stalling))
    }
}

#[derive(Debug)]
struct Stalling<T>(T);

fn capped(timeout: NextTimeout) -> NextTimeout {
    if timeout.after.is_not_happening() || *timeout.after > STALL_TIMEOUT {
        NextTimeout {
            after: time::Duration::Exact(STALL_TIMEOUT),
            reason: timeout.reason,
        }
    } else {
        timeout
    }
}

impl<T: Transport> Transport for Stalling<T> {
    fn buffers(&mut self) -> &mut dyn Buffers {
        self.0.buffers()
    }

    fn transmit_output(&mut self, amount: usize, timeout: NextTimeout) -> Result<(), ureq::Error> {
        self.0.transmit_output(amount, capped(timeout))
    }

    fn await_input(&mut self, timeout: NextTimeout) -> Result<bool, ureq::Error> {
        self.0.await_input(capped(timeout))
    }

    fn is_open(&mut self) -> bool {
        self.0.is_open()
    }

    fn is_tls(&self) -> bool {
        self.0.is_tls()
    }
}

pub fn is_expired(err: &io::Error) -> bool {
    err.kind() == io::ErrorKind::PermissionDenied
}

pub fn is_cancelled(err: &io::Error) -> bool {
    err.kind() == io::ErrorKind::Other && err.to_string() == CANCELLED
}

const CANCELLED: &str = "cancelled";

pub struct HttpSource {
    agent: Agent,
    url: String,
    cancel: Arc<AtomicBool>,
    len: Option<u64>,
    pos: u64,
    reader: Option<ureq::BodyReader<'static>>,
    reader_pos: u64,
    content_type: Option<String>,
}

fn header<'a>(response: &'a Response<ureq::Body>, name: &str) -> Option<&'a str> {
    response.headers().get(name).and_then(|v| v.to_str().ok())
}

fn backoff(attempt: u32) {
    thread::sleep(Duration::from_millis(250 * u64::from(attempt)));
}

impl HttpSource {
    pub fn open(agent: Agent, url: String, cancel: Arc<AtomicBool>) -> io::Result<Self> {
        let mut source = HttpSource {
            agent,
            url,
            cancel,
            len: None,
            pos: 0,
            reader: None,
            reader_pos: 0,
            content_type: None,
        };
        let mut attempt = 0;
        loop {
            match source.connect(0) {
                Ok(()) => return Ok(source),
                Err(err) if retryable(&err) && attempt + 1 < MAX_ATTEMPTS => {
                    attempt += 1;
                    backoff(attempt);
                }
                Err(err) => return Err(err),
            }
        }
    }

    pub fn content_type(&self) -> Option<String> {
        self.content_type.clone()
    }

    fn check_cancel(&self) -> io::Result<()> {
        if self.cancel.load(Ordering::Relaxed) {
            return Err(io::Error::other(CANCELLED));
        }
        Ok(())
    }

    fn connect(&mut self, from: u64) -> io::Result<()> {
        self.check_cancel()?;
        self.reader = None;
        self.reader_pos = from;
        if self.len.is_some_and(|len| from >= len) {
            return Ok(());
        }
        let response = self
            .agent
            .get(&self.url)
            .header("Range", &format!("bytes={from}-"))
            .call()
            .map_err(io::Error::other)?;
        match response.status().as_u16() {
            206 => {
                if let Some(total) = header(&response, "content-range")
                    .and_then(|v| v.rsplit('/').next())
                    .and_then(|v| v.trim().parse().ok())
                {
                    self.len = Some(total);
                }
                if let Some(start) = header(&response, "content-range")
                    .and_then(|v| v.split_whitespace().nth(1))
                    .and_then(|v| v.split('-').next())
                    .and_then(|v| v.parse().ok())
                {
                    self.reader_pos = start;
                }
            }
            200 => {
                if self.len.is_none() {
                    self.len = header(&response, "content-length").and_then(|v| v.parse().ok());
                }
                self.reader_pos = 0;
            }
            416 => return Ok(()),
            401 | 403 => {
                return Err(io::Error::new(
                    io::ErrorKind::PermissionDenied,
                    "the link to this track expired",
                ));
            }
            404 => {
                return Err(io::Error::new(
                    io::ErrorKind::NotFound,
                    "this track isn't on the server any more",
                ));
            }
            status => return Err(io::Error::other(format!("the server answered {status}"))),
        }
        if self.content_type.is_none() {
            self.content_type = header(&response, "content-type").map(str::to_string);
        }
        self.reader = Some(response.into_body().into_reader());
        Ok(())
    }

    fn skip_to_pos(&mut self) -> io::Result<()> {
        let mut scratch = [0u8; 16 * 1024];
        while self.reader_pos < self.pos {
            self.check_cancel()?;
            let Some(reader) = self.reader.as_mut() else {
                return Ok(());
            };
            let want = ((self.pos - self.reader_pos) as usize).min(scratch.len());
            match reader.read(&mut scratch[..want]) {
                Ok(0) => {
                    self.reader = None;
                    return Ok(());
                }
                Ok(n) => self.reader_pos += n as u64,
                Err(_) => {
                    self.reader = None;
                    return Ok(());
                }
            }
        }
        Ok(())
    }
}

fn retryable(err: &io::Error) -> bool {
    !matches!(
        err.kind(),
        io::ErrorKind::PermissionDenied | io::ErrorKind::NotFound
    ) && !is_cancelled(err)
}

impl Read for HttpSource {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        if buf.is_empty() {
            return Ok(0);
        }
        let mut attempt = 0;
        loop {
            self.check_cancel()?;
            if self.len.is_some_and(|len| self.pos >= len) {
                return Ok(0);
            }
            if self.reader.is_some()
                && self.reader_pos < self.pos
                && self.pos - self.reader_pos <= SKIP_FORWARD_LIMIT
            {
                self.skip_to_pos()?;
            }
            if self.reader.is_none() || self.reader_pos != self.pos {
                if let Err(err) = self.connect(self.pos) {
                    attempt += 1;
                    if !retryable(&err) || attempt >= MAX_ATTEMPTS {
                        return Err(err);
                    }
                    backoff(attempt);
                    continue;
                }
                if self.reader.is_none() {
                    return Ok(0);
                }
                if self.reader_pos < self.pos {
                    self.skip_to_pos()?;
                }
                continue;
            }
            let Some(reader) = self.reader.as_mut() else {
                continue;
            };
            match reader.read(buf) {
                Ok(0) => {
                    self.reader = None;
                    if self.len.is_some_and(|len| self.pos < len) {
                        attempt += 1;
                        if attempt >= MAX_ATTEMPTS {
                            return Err(io::Error::new(
                                io::ErrorKind::UnexpectedEof,
                                "the connection closed early",
                            ));
                        }
                        backoff(attempt);
                        continue;
                    }
                    return Ok(0);
                }
                Ok(n) => {
                    self.pos += n as u64;
                    self.reader_pos += n as u64;
                    return Ok(n);
                }
                Err(err) => {
                    self.reader = None;
                    attempt += 1;
                    if attempt >= MAX_ATTEMPTS {
                        return Err(err);
                    }
                    backoff(attempt);
                }
            }
        }
    }
}

impl Seek for HttpSource {
    fn seek(&mut self, to: SeekFrom) -> io::Result<u64> {
        let target = match to {
            SeekFrom::Start(pos) => i128::from(pos),
            SeekFrom::Current(delta) => i128::from(self.pos) + i128::from(delta),
            SeekFrom::End(delta) => match self.len {
                Some(len) => i128::from(len) + i128::from(delta),
                None => {
                    return Err(io::Error::new(
                        io::ErrorKind::Unsupported,
                        "the stream's length is unknown",
                    ));
                }
            },
        };
        if target < 0 {
            return Err(io::Error::new(
                io::ErrorKind::InvalidInput,
                "seek before the start",
            ));
        }
        self.pos = target as u64;
        Ok(self.pos)
    }
}

impl MediaSource for HttpSource {
    fn is_seekable(&self) -> bool {
        self.len.is_some()
    }

    fn byte_len(&self) -> Option<u64> {
        self.len
    }
}
