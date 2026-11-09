package aigifmaker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"
)

func js(s string) map[string]any { var m map[string]any; _ = json.Unmarshal([]byte(s), &m); return m }

// mock speaks the real response shapes: every answer is HTTP 200 with a JSON code.
func mock(t *testing.T, queries []string) (*Client, *[]map[string]any) {
	t.Helper()
	var seen []map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		if b == nil {
			b = map[string]any{}
		}
		b["_path"] = r.URL.Path
		seen = append(seen, b)
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			w.Write([]byte(`{"code":-1,"message":"invalid API key — create one at https://aigifmaker.ai/settings/apikeys"}`))
			return
		}
		switch r.URL.Path {
		case "/api/ai/generate":
			w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"t1","status":"pending","taskId":"up-9","provider":"openlux"}}`))
		case "/api/ai/query":
			w.Write([]byte(queries[0]))
			if len(queries) > 1 {
				queries = queries[1:]
			}
		case "/api/user/get-user-credits":
			w.Write([]byte(`{"code":0,"message":"ok","data":{"remainingCredits":7,"made":3}}`))
		}
	}))
	t.Cleanup(s.Close)
	c := New("sk-test")
	c.BaseURL, c.PollEvery = s.URL, time.Millisecond
	return c, &seen
}

func TestSuccessAndRequestBody(t *testing.T) {
	c, seen := mock(t, []string{
		`{"code":0,"message":"ok","data":{"id":"t1","status":"processing","images":[]}}`,
		`{"code":0,"message":"ok","data":{"id":"t1","status":"success","images":["https://cdn.aigifmaker.ai/t1.gif"],"watermarked":[false]}}`,
	})
	url, err := c.Generate(context.Background(), "a cat dancing")
	if err != nil || url != "https://cdn.aigifmaker.ai/t1.gif" {
		t.Fatal(url, err)
	}
	want := RequestBody("a cat dancing")
	got := (*seen)[0]
	delete(got, "_path")
	wb, _ := json.Marshal(want)
	var wantJSON map[string]any
	_ = json.Unmarshal(wb, &wantJSON)
	if !reflect.DeepEqual(got, wantJSON) {
		t.Fatalf("submit body %v", got)
	}
	if got["model"] != "gif-maker-v1" || got["options"].(map[string]any)["output_format"] != "gif" {
		t.Fatal(got)
	}
	if q := (*seen)[1]; q["_path"] != "/api/ai/query" || q["taskId"] != "t1" {
		t.Fatalf("query body %v", q)
	}
	if n, err := c.Credits(context.Background()); n != 7 || err != nil {
		t.Fatal(n, err)
	}
}

func TestQueuedKeepsPolling(t *testing.T) {
	c, _ := mock(t, []string{
		`{"code":0,"message":"ok","data":{"id":"t1","status":"processing","images":[],"queued":true,"queueEta":40}}`,
		`{"code":0,"message":"ok","data":{"id":"t1","status":"processing","images":[],"queued":true,"queueEta":5}}`,
		`{"code":0,"message":"ok","data":{"id":"t1","status":"success","images":["https://cdn.aigifmaker.ai/q.gif"],"watermarked":[true]}}`,
	})
	st, err := c.Status(context.Background(), "t1")
	if err != nil || !st.Queued || st.QueueEta != 40 || st.Status != "processing" {
		t.Fatal(st, err)
	}
	if url, err := c.Wait(context.Background(), "t1"); err != nil || url != "https://cdn.aigifmaker.ai/q.gif" {
		t.Fatal(url, err)
	}
}

func TestFailed(t *testing.T) {
	c, _ := mock(t, []string{`{"code":0,"message":"ok","data":{"id":"t1","status":"failed","images":[],"errorMessage":"upstream error","refundNote":"credit refunded"}}`})
	_, err := c.Generate(context.Background(), "x")
	var e *Error
	if !errors.Is(err, ErrFailed) || !errors.As(err, &e) || e.Message != "upstream error" {
		t.Fatal(err)
	}
	st, _ := ParseStatus(200, js(`{"code":0,"message":"ok","data":{"id":"t1","status":"failed","errorMessage":"blocked","failureKind":"moderation"}}`))
	if _, done, err := Finished(st); !done || !errors.Is(err, ErrRejected) {
		t.Fatal(done, err)
	}
}

func TestInvalidKey(t *testing.T) {
	c, _ := mock(t, nil)
	c.APIKey = "sk-wrong"
	if _, err := c.Credits(context.Background()); !errors.Is(err, ErrAuth) {
		t.Fatal(err)
	}
	if _, err := ParseCredits(200, js(`{"code":-1,"message":"no auth, please sign in"}`)); !errors.Is(err, ErrAuth) {
		t.Fatal(err)
	}
}

func TestWall(t *testing.T) {
	_, err := ParseSubmit(200, js(`{"code":0,"data":{"wall":true,"reason":"anon_credits","cost":1,"remaining":0,"grant":0}}`))
	if !errors.Is(err, ErrAuth) {
		t.Fatal(err)
	}
}

func TestInsufficientCredits(t *testing.T) {
	_, err := ParseSubmit(200, js(`{"code":-1,"message":"insufficient credits"}`))
	var e *Error
	if !errors.Is(err, ErrQuota) || !errors.As(err, &e) || e.Code != -1 {
		t.Fatal(err)
	}
}

func TestCooldownAndGeneric(t *testing.T) {
	if _, err := ParseSubmit(200, js(`{"code":429,"message":"please wait before trying again"}`)); !errors.Is(err, ErrRejected) {
		t.Fatal(err)
	}
	_, err := ParseSubmit(200, js(`{"code":-1,"message":"something else broke"}`))
	var e *Error
	if !errors.Is(err, ErrAPI) || !errors.As(err, &e) || e.Message != "something else broke" {
		t.Fatal(err)
	}
	if _, err := New("sk-x").Submit(context.Background(), "  "); !errors.Is(err, ErrAPI) {
		t.Fatal(err)
	}
}

// Live, free (no generation): AIGIFMAKER_LIVE=1 go test -run Live
func TestLiveSmoke(t *testing.T) {
	if os.Getenv("AIGIFMAKER_LIVE") == "" {
		t.Skip("set AIGIFMAKER_LIVE=1")
	}
	ctx := context.Background()
	if _, err := New("sk-invalidtest000").Credits(ctx); !errors.Is(err, ErrAuth) {
		t.Fatal("wrong key:", err)
	}
	anon := &Client{BaseURL: BaseURL, HTTP: &http.Client{Timeout: 30 * time.Second}}
	if _, err := anon.Submit(ctx, "a cat waving"); !errors.Is(err, ErrAuth) {
		t.Fatal("no key:", err)
	}
}
