// Package transcribe turns speech into text on the agent, so the browser
// never holds a key. The browser sends a WAV file to POST /kit/transcribe.
package transcribe

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// DefaultModel is a cheap audio model on OpenRouter.
const DefaultModel = "google/gemini-3.5-flash-lite"

const prompt = "Transcribe this audio exactly, in the language spoken. Reply with the transcript only. If there is no speech, reply with nothing."

// MaxBytes is the largest WAV file that the endpoint takes (about 5 minutes
// of 16 kHz mono 16-bit audio).
const MaxBytes = 10 << 20

// Transcriber turns a WAV file into text.
type Transcriber interface {
	Transcribe(ctx context.Context, wav []byte) (string, error)
}

// OpenRouter transcribes with an audio model through the chat completions API.
type OpenRouter struct {
	APIKey  string
	BaseURL string // default https://openrouter.ai/api/v1
	Model   string // default DefaultModel
	Client  *http.Client
}

// A model marks a recording with no speech as "<noise>", "[silence]" and so on.
var noSpeech = regexp.MustCompile(`^[<\[(][^<>\[\]()]*[>\])]\.?$`)

func (o OpenRouter) Transcribe(ctx context.Context, wav []byte) (string, error) {
	base, model, client := o.BaseURL, o.Model, o.Client
	if base == "" {
		base = "https://openrouter.ai/api/v1"
	}
	if model == "" {
		model = DefaultModel
	}
	if client == nil {
		client = http.DefaultClient
	}
	body, _ := json.Marshal(map[string]any{
		"model":       model,
		"temperature": 0,
		"messages": []any{map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": prompt},
			map[string]any{"type": "input_audio", "input_audio": map[string]any{"data": base64.StdEncoding.EncodeToString(wav), "format": "wav"}},
		}}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+o.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Title", "agent-kit")
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("speech to text failed: %w", err)
	}
	defer res.Body.Close()
	var out struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out)
	if res.StatusCode != http.StatusOK {
		msg := res.Status
		if out.Error != nil && out.Error.Message != "" {
			msg = out.Error.Message
		}
		return "", fmt.Errorf("speech to text failed (%d): %s", res.StatusCode, msg)
	}
	if len(out.Choices) == 0 {
		return "", errors.New("speech to text failed: no choices")
	}
	said, _ := out.Choices[0].Message.Content.(string)
	said = strings.TrimSpace(said)
	if noSpeech.MatchString(said) {
		return "", nil
	}
	return said, nil
}

// Fake gives fixed text for a valid WAV file. It is for tests and for the
// dev agent without a key.
type Fake struct{ Text string }

func (f Fake) Transcribe(_ context.Context, wav []byte) (string, error) {
	if _, err := CheckWAV(wav); err != nil {
		return "", err
	}
	return f.Text, nil
}

// WAVInfo is what CheckWAV reads from the header.
type WAVInfo struct {
	Channels, SampleRate, Bits int
	Seconds                    float64
}

// CheckWAV checks that b is a PCM WAV file with some audio in it.
func CheckWAV(b []byte) (WAVInfo, error) {
	if len(b) < 44 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return WAVInfo{}, errors.New("not a WAV file")
	}
	var info WAVInfo
	for off := 12; off+8 <= len(b); {
		id, size := string(b[off:off+4]), int(binary.LittleEndian.Uint32(b[off+4:off+8]))
		data := b[off+8:]
		switch id {
		case "fmt ":
			if size < 16 || len(data) < 16 || binary.LittleEndian.Uint16(data[0:2]) != 1 {
				return WAVInfo{}, errors.New("the WAV file is not PCM")
			}
			info.Channels = int(binary.LittleEndian.Uint16(data[2:4]))
			info.SampleRate = int(binary.LittleEndian.Uint32(data[4:8]))
			info.Bits = int(binary.LittleEndian.Uint16(data[14:16]))
		case "data":
			if info.SampleRate == 0 || info.Channels == 0 || info.Bits == 0 {
				return WAVInfo{}, errors.New("the WAV file has no format chunk")
			}
			n := min(size, len(data))
			info.Seconds = float64(n) / float64(info.SampleRate*info.Channels*info.Bits/8)
			if n == 0 {
				return WAVInfo{}, errors.New("the WAV file has no audio")
			}
			return info, nil
		}
		off += 8 + size + size%2
	}
	return WAVInfo{}, errors.New("the WAV file has no data chunk")
}

// Handler serves POST /kit/transcribe. The body is a WAV file. The answer is
// {"text": "..."}.
func Handler(t Transcriber) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST a WAV file", http.StatusMethodNotAllowed)
			return
		}
		wav, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBytes))
		if err != nil {
			http.Error(w, "the recording is too long", http.StatusRequestEntityTooLarge)
			return
		}
		if _, err := CheckWAV(wav); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		text, err := t.Transcribe(r.Context(), wav)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"text": text})
	})
}
