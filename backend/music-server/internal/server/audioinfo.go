package server

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"
)

const (
	audioInfoQueueDepth = 4096
	audioInfoTimeout    = 30 * time.Second
	audioInfoUnknown    = "unknown"
)

var (
	audioInfoQueue = make(chan string, audioInfoQueueDepth)
	audioInfoOn    atomic.Bool
)

type audioInfo struct {
	Codec      string
	Kbps       int
	SampleRate int
	Channels   int
	BitDepth   int
}

func ffprobeBinary() string {
	bin := ffmpegBinary()
	if bin == "" {
		return ""
	}
	probe := filepath.Join(filepath.Dir(bin), "ffprobe")
	if _, err := os.Stat(probe); err == nil {
		return probe
	}
	if p, err := exec.LookPath("ffprobe"); err == nil {
		return p
	}
	return ""
}

func (s *Server) queueAudioInfo(trackID string) {
	if !audioInfoOn.Load() {
		return
	}
	select {
	case audioInfoQueue <- trackID:
	default:
		log.Printf("audio info: queue full, deferring track %s to next start", trackID)
	}
}

func (s *Server) startAudioInfoWorker() {
	probe := ffprobeBinary()
	if probe == "" {
		log.Printf("music-server audio info: ffprobe not found, formats and bitrates won't be shown")
		return
	}
	audioInfoOn.Store(true)
	go func() {
		for id := range audioInfoQueue {
			func() {
				defer logPanic("audio info of track " + id)
				s.probeTrackAudio(probe, id)
			}()
		}
	}()
	go s.backfillAudioInfo()
}

func (s *Server) backfillAudioInfo() {
	rows, err := s.db.Query(`SELECT id FROM tracks WHERE codec = '' ORDER BY created_at DESC`)
	if err != nil {
		logErr("audio info backfill", err)
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if len(ids) == 0 {
		return
	}
	log.Printf("audio info: reading the format of %d track(s) in the background", len(ids))
	for _, id := range ids {
		audioInfoQueue <- id
	}
}

func (s *Server) probeTrackAudio(probe, trackID string) {
	var rel string
	if err := s.db.QueryRow(`SELECT file_path FROM tracks WHERE id = ?`, trackID).Scan(&rel); err != nil {
		return
	}
	info, err := probeAudio(probe, filepath.Join(s.dataDir, rel))
	if err != nil {
		if _, statErr := os.Stat(filepath.Join(s.dataDir, rel)); statErr != nil {
			return
		}
		logErr("audio info "+trackID, err)
		info = audioInfo{Codec: audioInfoUnknown}
	}
	_, err = s.db.Exec(`UPDATE tracks SET codec = ?, bitrate = ?, sample_rate = ?, channels = ?, bit_depth = ?, updated_at = ?
		WHERE id = ? AND file_path = ?`,
		info.Codec, info.Kbps, info.SampleRate, info.Channels, info.BitDepth, nowRFC3339(), trackID, rel)
	if err != nil {
		logErr("save audio info", err)
	}
}

func probeAudio(probe, path string) (audioInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), audioInfoTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, probe, "-v", "error", "-select_streams", "a:0",
		"-show_entries", "stream=codec_name,bit_rate,sample_rate,channels,bits_per_raw_sample,bits_per_sample:format=bit_rate,duration",
		"-of", "json", path).Output()
	if err != nil {
		return audioInfo{}, err
	}
	var parsed struct {
		Streams []struct {
			CodecName        string `json:"codec_name"`
			BitRate          string `json:"bit_rate"`
			SampleRate       string `json:"sample_rate"`
			Channels         int    `json:"channels"`
			BitsPerRawSample string `json:"bits_per_raw_sample"`
			BitsPerSample    int    `json:"bits_per_sample"`
		} `json:"streams"`
		Format struct {
			BitRate  string `json:"bit_rate"`
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return audioInfo{}, err
	}
	if len(parsed.Streams) == 0 {
		return audioInfo{Codec: audioInfoUnknown}, nil
	}
	st := parsed.Streams[0]
	info := audioInfo{Codec: st.CodecName, Channels: st.Channels}
	if info.Codec == "" {
		info.Codec = audioInfoUnknown
	}
	info.SampleRate, _ = strconv.Atoi(st.SampleRate)
	info.BitDepth, _ = strconv.Atoi(st.BitsPerRawSample)
	if info.BitDepth == 0 {
		info.BitDepth = st.BitsPerSample
	}
	bps, _ := strconv.ParseFloat(st.BitRate, 64)
	if bps == 0 {
		bps, _ = strconv.ParseFloat(parsed.Format.BitRate, 64)
	}
	info.Kbps = int(math.Round(bps / 1000))
	return info, nil
}
