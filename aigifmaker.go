// Package aigifmaker is the official Go SDK for AI GIF Maker
// (https://aigifmaker.ai): turn text into animated GIFs.
//
// Turn text into animated GIFs with AI GIF Maker. Native GIF generation — no
// video conversion. Type what you want and download your GIF in about a minute.
//
// Create an API key at https://aigifmaker.ai/settings/apikeys and set
// AIGIFMAKER_API_KEY, then:
//
//	url, err := aigifmaker.Generate(ctx, "a cat dancing in the rain, pixel art")
//
// Generate submits the prompt, polls until the GIF is ready and returns its
// URL. Each GIF costs 1 credit on your AI GIF Maker account; failed runs are
// refunded. Standard library only.
package aigifmaker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Version is the SDK version sent in the User-Agent header.
const Version = "0.1.1"

// BaseURL is the service this package talks to.
const BaseURL = "https://aigifmaker.ai"

// KeysURL is where an API key is created.
const KeysURL = "https://aigifmaker.ai/settings/apikeys"

// PricingURL is where credits are bought.
const PricingURL = "https://aigifmaker.ai/pricing"

// EnvKey is the environment variable read when no key is passed.
const EnvKey = "AIGIFMAKER_API_KEY"

// Sentinel errors; test with errors.Is. The concrete error is *Error.
var (
	ErrAuth     = errors.New("aigifmaker: API key missing or invalid")
	ErrQuota    = errors.New("aigifmaker: insufficient credits")
	ErrRejected = errors.New("aigifmaker: the prompt was refused by moderation")
	ErrFailed   = errors.New("aigifmaker: generation failed")
	ErrTimeout  = errors.New("aigifmaker: deadline passed; the GIF may still finish in your history")
	ErrAPI      = errors.New("aigifmaker: request failed")
)

// Error carries the API message and, for failed runs, the failure kind.
type Error struct {
	Kind        error
	Code        int    // the JSON "code" of the answer (0, -1 or another value)
	Message     string // the API message, verbatim
	FailureKind string // for failed runs, e.g. "moderation"
}

func (e *Error) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%v: %s", e.Kind, e.Message)
	}
	return e.Kind.Error()
}

func (e *Error) Unwrap() error { return e.Kind }

// Status is one task as POST /api/ai/query reports it.
type Status struct {
	ID           string         `json:"id"`
	Status       string         `json:"status"` // pending | processing | success | failed | canceled
	Images       []string       `json:"images"`
	Queued       bool           `json:"queued"`
	QueueEta     float64        `json:"queueEta"` // seconds, when Queued
	ErrorMessage string         `json:"errorMessage"`
	FailureKind  string         `json:"failureKind"`
	RefundNote   string         `json:"refundNote"`
	Raw          map[string]any `json:"-"`
}

// URL is the finished GIF, or "" while the task runs.
func (s *Status) URL() string {
	if len(s.Images) > 0 {
		return s.Images[0]
	}
	return ""
}

// Client is bound to one API key.
type Client struct {
	APIKey    string
	BaseURL   string
	HTTP      *http.Client
	PollEvery time.Duration // default 3s
	Timeout   time.Duration // total wait in Generate; default 15 minutes
}

// New returns a client. An empty key reads AIGIFMAKER_API_KEY.
func New(apiKey string) *Client {
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv(EnvKey))
	}
	return &Client{APIKey: apiKey, BaseURL: BaseURL, HTTP: &http.Client{Timeout: 60 * time.Second},
		PollEvery: 3 * time.Second, Timeout: 15 * time.Minute}
}

// Generate turns a prompt into an animated GIF with the key in
// AIGIFMAKER_API_KEY and returns the GIF URL.
func Generate(ctx context.Context, prompt string) (string, error) {
	return New("").Generate(ctx, prompt)
}

// RequestBody is the exact JSON sent to POST /api/ai/generate. Only the prompt varies.
func RequestBody(prompt string) map[string]any {
	return map[string]any{
		"provider":  "openlux",
		"mediaType": "image",
		"model":     "gif-maker-v1",
		"scene":     "text-to-image",
		"prompt":    prompt,
		"options":   map[string]any{"output_format": "gif"},
	}
}

// Submit starts a GIF and returns its task id without waiting.
func (c *Client) Submit(ctx context.Context, prompt string) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", &Error{Kind: ErrAPI, Code: -1, Message: "prompt is required"}
	}
	status, body, err := c.post(ctx, "/api/ai/generate", RequestBody(prompt))
	if err != nil {
		return "", err
	}
	return ParseSubmit(status, body)
}

// Status looks up a task.
func (c *Client) Status(ctx context.Context, id string) (*Status, error) {
	status, body, err := c.post(ctx, "/api/ai/query", map[string]any{"taskId": id})
	if err != nil {
		return nil, err
	}
	return ParseStatus(status, body)
}

// Credits returns the credits left on the account. Free to call.
func (c *Client) Credits(ctx context.Context) (int, error) {
	status, body, err := c.post(ctx, "/api/user/get-user-credits", nil)
	if err != nil {
		return 0, err
	}
	return ParseCredits(status, body)
}

// Generate submits a prompt, waits for the GIF and returns its URL.
func (c *Client) Generate(ctx context.Context, prompt string) (string, error) {
	id, err := c.Submit(ctx, prompt)
	if err != nil {
		return "", err
	}
	return c.Wait(ctx, id)
}

// Wait polls a submitted task until it finishes and returns the GIF URL.
// A queued task (accounts that have not bought credits yet) keeps polling.
func (c *Client) Wait(ctx context.Context, id string) (string, error) {
	deadline := time.Now().Add(c.Timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(c.PollEvery):
		}
		st, err := c.Status(ctx, id)
		if err != nil {
			if errors.Is(err, ErrAuth) {
				return "", err
			}
			continue // a dropped poll is not a failed run
		}
		if url, done, err := Finished(st); done {
			return url, err
		}
	}
	return "", &Error{Kind: ErrTimeout, Message: "task " + id}
}

// Finished reports whether a task is over, with its GIF URL or the failure.
func Finished(st *Status) (url string, done bool, err error) {
	switch st.Status {
	case "success":
		if u := st.URL(); u != "" {
			return u, true, nil
		}
		return "", true, &Error{Kind: ErrAPI, Message: "finished without a GIF URL"}
	case "failed", "canceled":
		msg := st.ErrorMessage
		if msg == "" {
			msg = st.Status
		}
		kind := ErrFailed
		if st.FailureKind == "moderation" {
			kind = ErrRejected
		}
		return "", true, &Error{Kind: kind, Code: 0, Message: msg, FailureKind: st.FailureKind}
	}
	return "", false, nil
}

func (c *Client) post(ctx context.Context, path string, payload any) (int, map[string]any, error) {
	var rd *bytes.Reader
	if payload == nil {
		rd = bytes.NewReader(nil)
	} else {
		b, _ := json.Marshal(payload)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+path, rd)
	if err != nil {
		return 0, nil, err
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "aigifmaker-go/"+Version)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, &Error{Kind: ErrAPI, Message: err.Error()}
	}
	defer res.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(res.Body).Decode(&body)
	return res.StatusCode, body, nil
}

// Envelope unwraps {"code":0,"message":"ok","data":{…}} and returns data, or
// the *Error the answer stands for.
func Envelope(status int, body map[string]any) (map[string]any, error) {
	if body == nil {
		return nil, &Error{Kind: ErrAPI, Message: fmt.Sprintf("HTTP %d with no JSON body", status)}
	}
	code, ok := body["code"].(float64)
	msg, _ := body["message"].(string)
	if !ok {
		return nil, &Error{Kind: ErrAPI, Message: fmt.Sprintf("HTTP %d: unexpected answer", status)}
	}
	if code == 0 {
		d, _ := body["data"].(map[string]any)
		if d == nil {
			d = map[string]any{}
		}
		if wall, _ := d["wall"].(bool); wall {
			return nil, &Error{Kind: ErrAuth, Code: 0, Message: "an API key is required; create one at " + KeysURL}
		}
		return d, nil
	}
	return nil, Classify(int(code), msg)
}

// Classify maps a non-zero JSON code and its message to an *Error.
func Classify(code int, msg string) error {
	low := strings.ToLower(msg)
	kind := ErrAPI
	switch {
	case code != -1:
		kind = ErrRejected // content cooldown and similar
	case strings.Contains(low, "invalid api key") || strings.Contains(low, "no auth"):
		kind = ErrAuth
	case strings.Contains(low, "insufficient credits"):
		kind = ErrQuota
		msg += " (buy credits at " + PricingURL + ")"
	}
	if msg == "" {
		msg = "request failed"
	}
	return &Error{Kind: kind, Code: code, Message: msg}
}

// ParseSubmit reads the answer of POST /api/ai/generate and returns the task id.
func ParseSubmit(status int, body map[string]any) (string, error) {
	d, err := Envelope(status, body)
	if err != nil {
		return "", err
	}
	if id, _ := d["id"].(string); id != "" {
		return id, nil
	}
	return "", &Error{Kind: ErrAPI, Message: "the service returned no task id"}
}

// ParseStatus reads the answer of POST /api/ai/query.
func ParseStatus(status int, body map[string]any) (*Status, error) {
	d, err := Envelope(status, body)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(d)
	var st Status
	_ = json.Unmarshal(b, &st)
	st.Raw = d
	return &st, nil
}

// ParseCredits reads the answer of POST /api/user/get-user-credits.
func ParseCredits(status int, body map[string]any) (int, error) {
	d, err := Envelope(status, body)
	if err != nil {
		return 0, err
	}
	if n, ok := d["remainingCredits"].(float64); ok {
		return int(n), nil
	}
	return 0, &Error{Kind: ErrAPI, Message: "no remainingCredits in the answer"}
}
