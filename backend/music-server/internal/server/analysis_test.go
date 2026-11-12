package server

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func separateSilence(t *testing.T, bin, path string, p silenceParams) SilenceInfo {
	t.Helper()
	out, err := exec.Command(bin, "-nostdin", "-hide_banner", "-nostats", "-i", path,
		"-af", "silencedetect=noise="+p.noise+":d="+p.minSeconds, "-f", "null", "-").CombinedOutput()
	if err != nil {
		t.Fatalf("silencedetect: %v: %s", err, out)
	}
	return parseSilence(string(out), 0)
}

func separateLoudness(t *testing.T, bin, path string) (float64, error) {
	t.Helper()
	var stderr bytes.Buffer
	cmd := exec.Command(bin, "-nostdin", "-hide_banner", "-loglevel", "info",
		"-i", path, "-vn", "-af", "ebur128=framelog=quiet", "-f", "null", "-")
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("ebur128: %v: %s", err, stderr.String())
	}
	return parseLoudness(stderr.String())
}

func separatePeaks(t *testing.T, bin, path string) ([]int, error) {
	t.Helper()
	raw, err := exec.Command(bin, "-nostdin", "-hide_banner", "-loglevel", "error",
		"-i", path, "-vn", "-ac", "1", "-ar", strconv.Itoa(waveformSampleRate), "-f", "s16le", "-").Output()
	if err != nil {
		t.Fatalf("waveform: %v", err)
	}
	return waveformPeaks(raw)
}

func TestScanAudioMatchesSeparatePasses(t *testing.T) {
	bin := ffmpegBinary()
	if bin == "" {
		t.Skip("ffmpeg is not installed")
	}
	dir := t.TempDir()
	tone := "sine=frequency=440:sample_rate=44100:duration=6,tremolo=f=0.4:d=0.9"
	sources := map[string]string{
		"padded": tone + ",adelay=1200:all=1,apad=pad_dur=1.5",
		"plain":  tone,
		"quiet":  "anullsrc=r=44100:cl=stereo:d=4",
	}
	formats := []struct {
		ext  string
		args []string
	}{
		{".wav", []string{"-c:a", "pcm_s16le"}},
		{".flac", []string{"-c:a", "flac"}},
		{".m4a", []string{"-c:a", "aac", "-b:a", "128k"}},
		{".mp3", []string{"-c:a", "libmp3lame", "-b:a", "128k"}},
		{".ogg", []string{"-c:a", "libvorbis"}},
		{".opus", []string{"-c:a", "libopus"}},
	}
	rule := silenceParams{noise: defaultTrimRule.ffmpegThreshold(), minSeconds: defaultTrimRule.ffmpegMinSeconds()}
	compared := 0
	for name, source := range sources {
		for _, f := range formats {
			path := filepath.Join(dir, name+f.ext)
			args := append([]string{"-nostdin", "-loglevel", "error", "-y", "-f", "lavfi", "-i", source, "-ac", "2"}, f.args...)
			if out, err := exec.Command(bin, append(args, path)...).CombinedOutput(); err != nil {
				t.Logf("skipping %s%s: this ffmpeg can't encode it: %s", name, f.ext, bytes.TrimSpace(out))
				continue
			}
			scan := scanAudio(context.Background(), bin, path, &rule, 0)
			if scan.silenceErr != nil {
				t.Fatalf("%s%s: scan failed: %v", name, f.ext, scan.silenceErr)
			}
			if want := separateSilence(t, bin, path, rule); scan.silence != want {
				t.Errorf("%s%s: silence = %+v, separate pass gave %+v", name, f.ext, scan.silence, want)
			}
			wantLoud, wantLoudErr := separateLoudness(t, bin, path)
			if scan.loudness != wantLoud || (scan.loudnessErr == nil) != (wantLoudErr == nil) {
				t.Errorf("%s%s: loudness = %v (%v), separate pass gave %v (%v)", name, f.ext, scan.loudness, scan.loudnessErr, wantLoud, wantLoudErr)
			}
			wantPeaks, wantPeaksErr := separatePeaks(t, bin, path)
			if !reflect.DeepEqual(scan.peaks, wantPeaks) || (scan.peaksErr == nil) != (wantPeaksErr == nil) {
				t.Errorf("%s%s: waveform differs from the separate pass", name, f.ext)
			}
			plain := scanAudio(context.Background(), bin, path, nil, 0)
			if plain.loudness != scan.loudness || !reflect.DeepEqual(plain.peaks, scan.peaks) {
				t.Errorf("%s%s: the scan without silence detection gave different loudness or waveform", name, f.ext)
			}
			if name == "padded" {
				start, end, cut := planTrim(scan.silence, defaultTrimRule)
				if !cut || start < 1 || scan.silence.Duration-end < 1.2 {
					t.Errorf("%s%s: expected the padding to be cut, got start %.2f end %.2f of %.2f (cut %v)", name, f.ext, start, end, scan.silence.Duration, cut)
				}
			}
			compared++
		}
	}
	if compared == 0 {
		t.Skip("this ffmpeg couldn't encode any test file")
	}
	t.Logf("compared %d files", compared)
}
