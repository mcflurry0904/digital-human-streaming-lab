package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var mediaID = regexp.MustCompile("^[0-9a-f]{32}$")

type Media struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Extension  string  `json:"extension"`
	Size       int64   `json:"size"`
	Duration   float64 `json:"duration"`
	AudioCodec string  `json:"audioCodec,omitempty"`
	VideoCodec string  `json:"videoCodec,omitempty"`
	SampleRate int     `json:"sampleRate,omitempty"`
	Channels   int     `json:"channels,omitempty"`
	Width      int     `json:"width,omitempty"`
	Height     int     `json:"height,omitempty"`
}

func newID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(raw[:])
}

func (s *server) readMedia(id string) (Media, error) {
	var media Media
	if !mediaID.MatchString(id) {
		return media, os.ErrNotExist
	}
	data, err := os.ReadFile(filepath.Join(s.dir, id, "metadata.json"))
	if err != nil {
		return media, err
	}
	err = json.Unmarshal(data, &media)
	if err == nil && (media.ID != id || !allowedExtension(s.kind, media.Extension)) {
		err = errors.New("invalid stored metadata")
	}
	return media, err
}

func (s *server) mediaPath(media Media) string {
	return filepath.Join(s.dir, media.ID, "source"+media.Extension)
}

func allowedExtension(kind, ext string) bool {
	return kind == "audio" && (ext == ".mp3" || ext == ".wav") || kind == "video" && ext == ".mp4"
}

func inspect(ctx context.Context, path string, media *Media) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	demuxer := map[string]string{".mp3": "mp3", ".wav": "wav", ".mp4": "mov"}[media.Extension]
	data, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-protocol_whitelist", "file,pipe", "-f", demuxer,
		"-show_entries", "format=format_name,duration:stream=codec_name,codec_type,sample_rate,channels,width,height:stream_disposition=attached_pic",
		"-of", "json", path).Output()
	if err != nil {
		return errors.New("FFprobe could not read the media")
	}
	var probe struct {
		Format struct {
			Name     string `json:"format_name"`
			Duration string
		}
		Streams []struct {
			Codec                   string `json:"codec_name"`
			Kind                    string `json:"codec_type"`
			SampleRate              string `json:"sample_rate"`
			Channels, Width, Height int
			Disposition             struct {
				AttachedPic int `json:"attached_pic"`
			}
		}
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	expected := map[string]string{".mp3": "mp3", ".wav": "wav", ".mp4": "mp4"}[media.Extension]
	formatOK := false
	for _, format := range strings.Split(probe.Format.Name, ",") {
		if format == expected {
			formatOK = true
		}
	}
	media.Duration, _ = strconv.ParseFloat(probe.Format.Duration, 64)
	if !formatOK || media.Duration <= 0 || math.IsNaN(media.Duration) || math.IsInf(media.Duration, 0) {
		return errors.New("media container or duration is invalid")
	}
	for _, stream := range probe.Streams {
		switch {
		case stream.Kind == "audio" && media.AudioCodec == "":
			media.AudioCodec, media.Channels = stream.Codec, stream.Channels
			media.SampleRate, _ = strconv.Atoi(stream.SampleRate)
		case stream.Kind == "video" && stream.Disposition.AttachedPic == 0 && media.VideoCodec == "":
			media.VideoCodec, media.Width, media.Height = stream.Codec, stream.Width, stream.Height
		}
	}
	if expected != "mp4" && media.AudioCodec == "" || expected == "mp4" && (media.Width == 0 || media.Height == 0) {
		return errors.New("required media tracks are missing")
	}
	return nil
}

func (s *server) list(w http.ResponseWriter, _ *http.Request) {
	s.actions.Lock()
	defer s.actions.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		fail(w, 500, "media storage unavailable")
		return
	}
	items := make([]Media, 0)
	for _, entry := range entries {
		if !entry.IsDir() || !mediaID.MatchString(entry.Name()) {
			continue
		}
		media, err := s.readMedia(entry.Name())
		if err != nil {
			fail(w, 500, "stored media metadata unavailable")
			return
		}
		items = append(items, media)
	}
	writeJSON(w, 200, items)
}

func (s *server) upload(w http.ResponseWriter, r *http.Request) {
	// Raw file body avoids multipart buffering and gives the configured limit exact semantics.
	if r.ContentLength > s.limit {
		fail(w, 413, "file too large")
		return
	}
	name := r.URL.Query().Get("name")
	ext := strings.ToLower(filepath.Ext(name))
	if name == "" || len(name) > 240 || strings.ContainsAny(name, "/\\\x00\r\n") || !allowedExtension(s.kind, ext) {
		fail(w, 400, "invalid filename or extension")
		return
	}
	media := Media{ID: newID(), Name: name, Extension: ext}
	staging, err := os.MkdirTemp(s.dir, ".upload-")
	if err != nil {
		fail(w, 500, "media storage unavailable")
		return
	}
	defer os.RemoveAll(staging)
	source := filepath.Join(staging, "source"+ext)
	file, err := os.OpenFile(source, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		fail(w, 500, "cannot create media file")
		return
	}
	media.Size, err = io.Copy(file, http.MaxBytesReader(w, r.Body, s.limit))
	closeErr := file.Close()
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		fail(w, 413, "file too large")
		return
	}
	if err != nil || closeErr != nil {
		fail(w, 400, "upload incomplete")
		return
	}
	if err = inspect(r.Context(), source, &media); err != nil {
		fail(w, 422, err.Error())
		return
	}
	data, err := json.Marshal(media)
	if err == nil {
		err = os.WriteFile(filepath.Join(staging, "metadata.json"), data, 0600)
	}
	if err == nil {
		s.actions.Lock()
		err = os.Rename(staging, filepath.Join(s.dir, media.ID))
		s.actions.Unlock()
	}
	if err != nil {
		fail(w, 500, "cannot commit media file")
		return
	}
	writeJSON(w, 201, media)
}

func (s *server) download(w http.ResponseWriter, r *http.Request) {
	media, err := s.readMedia(r.PathValue("id"))
	if err != nil {
		fail(w, 404, "media not found")
		return
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": media.Name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, s.mediaPath(media))
}

func (s *server) remove(w http.ResponseWriter, r *http.Request) {
	s.actions.Lock()
	defer s.actions.Unlock()
	media, err := s.readMedia(r.PathValue("id"))
	if err != nil {
		fail(w, 404, "media not found")
		return
	}
	status := s.snapshot()
	if status.MediaID == media.ID && (status.State == "running" || status.State == "starting") {
		fail(w, 409, "stop this media before deleting it")
		return
	}
	dir := filepath.Join(s.dir, media.ID)
	trash := filepath.Join(s.dir, ".deleted-"+media.ID)
	if err := os.Rename(dir, trash); err != nil {
		fail(w, 500, "cannot delete media")
		return
	}
	if err := os.RemoveAll(trash); err != nil {
		s.logger.Warn("media cleanup", "error", fmt.Sprint(err))
	}
	w.WriteHeader(204)
}
