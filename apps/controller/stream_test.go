package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestAudioLifecycle(t *testing.T) {
	for _, binary := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(binary); err != nil {
			t.Skipf("%s unavailable", binary)
		}
	}
	wav, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100",
		"-t", "0.137", "-f", "wav", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	// Patch the streamed WAV sizes to avoid relying on FFprobe duration estimation.
	for index := 12; index+8 < len(wav); {
		size := int(binary.LittleEndian.Uint32(wav[index+4 : index+8]))
		if string(wav[index:index+4]) == "data" {
			binary.LittleEndian.PutUint32(wav[index+4:index+8], uint32(len(wav)-index-8))
			break
		}
		index += 8 + size + size%2
	}
	binary.LittleEndian.PutUint32(wav[4:8], uint32(len(wav)-8))

	s := &server{kind: "audio", dir: t.TempDir(), limit: 1 << 20, logger: slog.Default(),
		status: Status{State: "idle"}, subs: make(map[chan packet]struct{})}
	test := httptest.NewServer(s.routes())
	defer test.Close()
	defer s.stop()

	call := func(method, path string, body io.Reader, code int) []byte {
		t.Helper()
		req, err := http.NewRequest(method, test.URL+path, body)
		if err != nil {
			t.Fatal(err)
		}
		res, err := test.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != code {
			t.Fatalf("%s %s got %d, expected %d: %s", method, path, res.StatusCode, code, data)
		}
		return data
	}

	call("POST", "/media?name=text.wav", strings.NewReader("invalid"), 422)
	call("POST", "/media?name=..%2Ftest.wav", bytes.NewReader(wav), 400)
	response := call("POST", "/media?name=tone.wav", bytes.NewReader(wav), 201)
	var media Media
	if err := json.Unmarshal(response, &media); err != nil || media.SampleRate != 44100 || media.ID == "" {
		t.Fatalf("unexpected metadata: %s (%v)", response, err)
	}
	if got := call("GET", "/media/"+media.ID, nil, 200); !bytes.Equal(got, wav) {
		t.Fatal("download differs from upload")
	}
	control := func(action string, loop bool, code int) Status {
		t.Helper()
		data, err := json.Marshal(map[string]any{"mediaId": media.ID, "loop": loop})
		if err != nil {
			t.Fatal(err)
		}
		result := call("POST", "/control/"+action, bytes.NewReader(data), code)
		var state Status
		if err := json.Unmarshal(result, &state); err != nil {
			t.Fatal(err)
		}
		return state
	}

	var clients []*websocket.Conn
	for range 2 {
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(test.URL, "http")+"/ws", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(8 * time.Second))
		if _, data, err := conn.ReadMessage(); err != nil || !bytes.Contains(data, []byte(`"hello"`)) {
			t.Fatalf("invalid hello: %s (%v)", data, err)
		}
		clients = append(clients, conn)
	}
	state := control("start", true, 202)
	if state.Clients != 2 {
		t.Fatalf("expected two subscribers: %+v", state)
	}
	call("DELETE", "/media/"+media.ID, nil, 409)

	const total = 20
	var readers sync.WaitGroup
	for _, conn := range clients {
		readers.Add(1)
		go func(conn *websocket.Conn) {
			defer readers.Done()
			kind, data, err := conn.ReadMessage()
			if err != nil || kind != websocket.TextMessage || !bytes.Contains(data, []byte(state.Session)) {
				t.Errorf("invalid start: %s (%v)", data, err)
				return
			}
			var first time.Time
			for seq := uint64(0); seq < total; seq++ {
				kind, data, err := conn.ReadMessage()
				if err != nil || kind != websocket.BinaryMessage || len(data) != 672 {
					t.Errorf("invalid frame %d, got %d bytes (%v)", seq, len(data), err)
					return
				}
				if hex.EncodeToString(data[:16]) != state.Session || binary.LittleEndian.Uint64(data[16:24]) != seq ||
					binary.LittleEndian.Uint64(data[24:32]) != seq*20 {
					t.Errorf("invalid session, sequence or PTS at frame %d", seq)
				}
				if seq == 0 {
					first = time.Now()
				}
			}
			if elapsed := time.Since(first); elapsed < 300*time.Millisecond || elapsed > time.Second {
				t.Errorf("expected paced frames, got %v", elapsed)
			}
		}(conn)
	}
	readers.Wait()
	restarted := control("restart", false, 202)
	if restarted.Session == state.Session || restarted.Loop {
		t.Fatalf("unexpected restart: %+v", restarted)
	}
	deadline := time.Now().Add(5 * time.Second)
	for s.snapshot().State != "ended" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if current := s.snapshot(); current.State != "ended" {
		t.Fatalf("one-shot stream did not end: %+v", current)
	}
	control("start", true, 202)
	if stopped := control("stop", false, 200); stopped.State != "stopped" {
		t.Fatalf("unexpected stop: %+v", stopped)
	}
	call("DELETE", "/media/"+media.ID, nil, 204)
	call("GET", "/media/"+media.ID, nil, 404)
}
