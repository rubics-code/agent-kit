package transcribe

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func wav(samples int) []byte {
	b := make([]byte, 44+samples*2)
	copy(b[0:], "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+samples*2))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 16000)
	binary.LittleEndian.PutUint32(b[28:], 32000)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(samples*2))
	return b
}

func TestCheckWAV(t *testing.T) {
	info, err := CheckWAV(wav(16000))
	if err != nil || info.SampleRate != 16000 || info.Seconds != 1 {
		t.Fatalf("%+v %v", info, err)
	}
	for _, bad := range [][]byte{nil, []byte("RIFF....WAVE"), wav(0)} {
		if _, err := CheckWAV(bad); err == nil {
			t.Fatalf("bad WAV passed: %q", bad)
		}
	}
}

func TestOpenRouterSendsAudioAndDropsNoise(t *testing.T) {
	var got map[string]any
	reply := "Hello there."
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("request: %s %v", r.URL.Path, r.Header)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": reply}}}})
	}))
	defer srv.Close()
	o := OpenRouter{APIKey: "test-key", BaseURL: srv.URL + "/v1"}
	text, err := o.Transcribe(t.Context(), wav(10))
	if err != nil || text != "Hello there." {
		t.Fatalf("%q %v", text, err)
	}
	if got["model"] != DefaultModel {
		t.Fatalf("model: %v", got["model"])
	}
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"format":"wav"`) {
		t.Fatalf("no audio: %s", b)
	}
	reply = "[silence]"
	if text, _ := o.Transcribe(t.Context(), wav(10)); text != "" {
		t.Fatalf("noise: %q", text)
	}
}

func TestOpenRouterError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		io.WriteString(w, `{"error":{"message":"no credit"}}`)
	}))
	defer srv.Close()
	_, err := OpenRouter{APIKey: "k", BaseURL: srv.URL}.Transcribe(t.Context(), wav(10))
	if err == nil || !strings.Contains(err.Error(), "no credit") {
		t.Fatalf("%v", err)
	}
}

func TestHandler(t *testing.T) {
	h := Handler(Fake{Text: "say hello"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/kit/transcribe", strings.NewReader(string(wav(100)))))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"text":"say hello"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/kit/transcribe", strings.NewReader("not audio")))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad body: %d", w.Code)
	}
}
