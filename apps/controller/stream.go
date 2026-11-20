package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const framePeriod = 20 * time.Millisecond
const frameBytes = 640

type Status struct {
	State    string  `json:"state"`
	MediaID  string  `json:"mediaId,omitempty"`
	Session  string  `json:"session,omitempty"`
	Loop     bool    `json:"loop"`
	Position float64 `json:"position"`
	Duration float64 `json:"duration"`
	Clients  int     `json:"clients"`
	Error    string  `json:"error,omitempty"`
}

type job struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type packet struct {
	kind int
	data []byte
}

func (s *server) snapshot() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.status
	status.Clients = len(s.subs)
	return status
}

func textPacket(value any) packet {
	data, _ := json.Marshal(value)
	return packet{websocket.TextMessage, data}
}

func startPacket(id string) packet {
	return textPacket(map[string]any{"type": "start", "session": id, "format": "s16le",
		"sampleRate": 16000, "channels": 1, "frameMs": 20, "frameBytes": frameBytes})
}

// Call with s.mu held. Each client has a bounded queue.
func (s *server) broadcast(message packet) {
	for client := range s.subs {
		select {
		case client <- message:
		default:
			delete(s.subs, client)
			close(client)
		}
	}
}

func (s *server) websocket(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{HandshakeTimeout: 5 * time.Second}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	queue := make(chan packet, 16)
	s.mu.Lock()
	s.subs[queue] = struct{}{}
	queue <- textPacket(map[string]any{"type": "hello", "version": 1, "state": s.status.State})
	if s.active != nil {
		queue <- startPacket(s.status.Session)
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subs, queue)
		s.mu.Unlock()
	}()

	disconnected := make(chan struct{})
	conn.SetReadLimit(1024)
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(30 * time.Second)) })
	go func() {
		defer close(disconnected)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	pings := time.NewTicker(10 * time.Second)
	defer pings.Stop()
	for {
		select {
		case <-disconnected:
			return
		case <-pings.C:
			if conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second)) != nil {
				return
			}
		case message, ok := <-queue:
			if !ok {
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
			if conn.WriteMessage(message.kind, message.data) != nil {
				return
			}
		}
	}
}

func (s *server) stop() {
	s.mu.Lock()
	active := s.active
	s.mu.Unlock()
	if active != nil {
		active.cancel()
		<-active.done
	}
}

func (s *server) control(w http.ResponseWriter, r *http.Request) {
	s.actions.Lock()
	defer s.actions.Unlock()
	action := r.PathValue("action")
	if action == "stop" {
		s.stop()
		writeJSON(w, 200, s.snapshot())
		return
	}
	if action != "start" && action != "restart" {
		fail(w, 404, "unknown control action")
		return
	}
	var input struct {
		MediaID string `json:"mediaId"`
		Loop    bool   `json:"loop"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	if decoder.Decode(&input) != nil {
		fail(w, 400, "invalid control payload")
		return
	}
	media, err := s.readMedia(input.MediaID)
	if err != nil {
		fail(w, 404, "media not found")
		return
	}
	status := s.snapshot()
	if action == "start" && (status.State == "starting" || status.State == "running") {
		fail(w, 409, "stream already active")
		return
	}
	s.stop()
	s.start(media, input.Loop)
	writeJSON(w, 202, s.snapshot())
}

func (s *server) start(media Media, loop bool) {
	ctx, cancel := context.WithCancel(context.Background())
	active := &job{cancel: cancel, done: make(chan struct{})}
	id := newID()
	s.mu.Lock()
	s.active = active
	s.status = Status{State: "starting", MediaID: media.ID, Session: id, Loop: loop, Duration: media.Duration}
	if s.kind == "audio" {
		s.broadcast(startPacket(id))
	}
	s.mu.Unlock()
	s.logger.Info("stream starting", "kind", s.kind, "session", id, "media", media.ID, "loop", loop)

	go func() {
		defer close(active.done)
		defer cancel()
		var err error
		if s.kind == "audio" {
			err = s.runAudio(ctx, media, id, loop)
		} else {
			err = s.runVideo(ctx, media, loop)
		}
		state, message := "ended", ""
		switch {
		case ctx.Err() != nil:
			state = "stopped"
		case err != nil:
			state, message = "error", err.Error()
		}
		s.mu.Lock()
		s.active = nil
		s.status.State, s.status.Error = state, message
		if s.kind == "audio" {
			s.broadcast(textPacket(map[string]string{"type": "end", "session": id, "reason": state, "error": message}))
		}
		s.mu.Unlock()
		s.logger.Info("stream finished", "kind", s.kind, "session", id, "state", state, "error", message)
	}()
}

func inputArgs(path string, loop, realtime bool) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	if realtime {
		args = append(args, "-re")
	}
	if loop {
		args = append(args, "-stream_loop", "-1")
	}
	var demuxer string
	switch {
	case strings.HasSuffix(path, ".wav"):
		demuxer = "wav"
	case strings.HasSuffix(path, ".mp3"):
		demuxer = "mp3"
	default:
		demuxer = "mov"
	}
	return append(args, "-protocol_whitelist", "file,pipe", "-f", demuxer, "-i", path)
}

func (s *server) position(media Media, loop bool, elapsed float64) {
	s.status.State = "running"
	if loop {
		elapsed = math.Mod(elapsed, media.Duration)
	}
	s.status.Position = math.Min(media.Duration, elapsed)
}

func audioPacket(session []byte, seq uint64, samples []byte) packet {
	data := make([]byte, 32+frameBytes)
	copy(data, session)
	binary.LittleEndian.PutUint64(data[16:24], seq)
	binary.LittleEndian.PutUint64(data[24:32], seq*20)
	copy(data[32:], samples)
	return packet{websocket.BinaryMessage, data}
}

func (s *server) runAudio(ctx context.Context, media Media, id string, loop bool) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	args := append(inputArgs(s.mediaPath(media), loop, false), "-map", "0:a:0", "-ac", "1",
		"-ar", "16000", "-c:a", "pcm_s16le", "-f", "s16le", "pipe:1")
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stderr tail
	cmd.Stderr = &stderr
	output, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	session, _ := hex.DecodeString(id)
	next := time.Now()
	for seq := uint64(0); ; seq++ {
		var samples [frameBytes]byte
		n, readErr := io.ReadFull(output, samples[:])
		if n > 0 {
			delay := time.Until(next)
			if delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-timer.C:
				case <-ctx.Done():
				}
				timer.Stop()
			}
			if ctx.Err() != nil {
				break
			}
			now := time.Now()
			// Correct ordinary timer jitter but avoid a burst after a decoder stall.
			if now.Sub(next) >= framePeriod {
				next = now
			}
			next = next.Add(framePeriod)
			s.mu.Lock()
			s.position(media, loop, float64(seq+1)*0.020)
			s.broadcast(audioPacket(session, seq, samples[:]))
			s.mu.Unlock()
		}
		if readErr != nil {
			if readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
				cancel()
				_ = cmd.Wait()
				return readErr
			}
			break
		}
	}
	if err := cmd.Wait(); err != nil {
		return ffmpegError(err, &stderr)
	}
	return nil
}

func videoArgs(path string, loop bool, rtsp string) []string {
	return append(inputArgs(path, loop, true), "-map", "0:V:0", "-an",
		"-vf", "scale=w='min(1280,iw)':h='min(720,ih)':force_original_aspect_ratio=decrease:force_divisible_by=2,format=yuv420p",
		"-c:v", "libx264", "-profile:v", "baseline", "-preset", "veryfast", "-tune", "zerolatency",
		"-r", "30", "-g", "30", "-bf", "0",
		"-progress", "pipe:1", "-nostats", "-f", "rtsp", "-rtsp_transport", "tcp", rtsp)
}

func (s *server) runVideo(ctx context.Context, media Media, loop bool) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg", videoArgs(s.mediaPath(media), loop, s.rtsp)...)
	var stderr tail
	cmd.Stderr = &stderr
	output, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	lines := bufio.NewScanner(output)
	for lines.Scan() {
		value, ok := strings.CutPrefix(lines.Text(), "out_time_us=")
		if !ok {
			continue
		}
		us, err := strconv.ParseInt(value, 10, 64)
		if err != nil || us <= 0 {
			continue
		}
		s.mu.Lock()
		s.position(media, loop, float64(us)/1e6)
		s.mu.Unlock()
	}
	if lines.Err() != nil {
		cancel()
	}
	if err := cmd.Wait(); err != nil {
		return ffmpegError(err, &stderr)
	}
	return lines.Err()
}

type tail struct{ data []byte }

func (t *tail) Write(data []byte) (int, error) {
	const limit = 2048
	n := len(data)
	switch {
	case n >= limit:
		t.data = append(t.data[:0], data[n-limit:]...)
	default:
		excess := len(t.data) + n - limit
		if excess > 0 {
			t.data = t.data[:copy(t.data, t.data[excess:])]
		}
		t.data = append(t.data, data...)
	}
	return n, nil
}

func ffmpegError(err error, stderr *tail) error {
	if message := bytes.TrimSpace(stderr.data); len(message) > 0 {
		return errors.New("FFmpeg: " + string(message))
	}
	return fmt.Errorf("FFmpeg: %w", err)
}
