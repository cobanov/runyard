package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type req struct {
	path, ctype string
	body        []byte
}

func TestChatChannels(t *testing.T) {
	got := make(chan req, 10)
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- req{r.URL.Path, r.Header.Get("Content-Type"), b}
	}))
	defer stub.Close()
	ctx := context.Background()
	m := message{Title: "gba/pixel32 finished", Body: "loss: 0.03 <ok> & done", URL: "https://runs.example/#/r/1"}
	withImage := m
	withImage.image, withImage.imageExt = []byte("png"), ".png"

	if err := (channel{Kind: "slack", Config: channelConfig{URL: stub.URL + "/slack"}}).send(ctx, m); err != nil {
		t.Fatal(err)
	}
	r := <-got
	var slack map[string]string
	json.Unmarshal(r.body, &slack)
	if r.path != "/slack" || slack["text"] != "*gba/pixel32 finished*\nloss: 0.03 &lt;ok&gt; &amp; done\n<https://runs.example/#/r/1|Open the run>" {
		t.Fatalf("slack: %s %s", r.path, r.body)
	}

	if err := (channel{Kind: "discord", Config: channelConfig{URL: stub.URL + "/discord"}}).send(ctx, withImage); err != nil {
		t.Fatal(err)
	}
	r = <-got
	if !strings.HasPrefix(r.ctype, "multipart/form-data") || !strings.Contains(string(r.body), `name="files[0]"; filename="sample.png"`) ||
		!strings.Contains(string(r.body), `{"content":"**gba/pixel32 finished**\nloss: 0.03 \u003cok\u003e \u0026 done\nhttps://runs.example/#/r/1"}`) {
		t.Fatalf("discord: %s %s", r.ctype, r.body)
	}

	tg := channel{Kind: "telegram", Config: channelConfig{URL: stub.URL, Token: "123:abc", Chat: "42"}}
	if err := tg.send(ctx, withImage); err != nil {
		t.Fatal(err)
	}
	if r = <-got; r.path != "/bot123:abc/sendPhoto" || !strings.Contains(string(r.body), "42") || !strings.Contains(string(r.body), `name="photo"`) {
		t.Fatalf("telegram photo: %s %s", r.path, r.body)
	}
	if err := tg.send(ctx, m); err != nil {
		t.Fatal(err)
	}
	if r = <-got; r.path != "/bot123:abc/sendMessage" || !strings.Contains(string(r.body), `"chat_id":"42"`) {
		t.Fatalf("telegram message: %s %s", r.path, r.body)
	}

	if cut(strings.Repeat("ç", 10), 5) != "çççç…" {
		t.Fatal("cut counts runes")
	}
}

func TestChannelAdd(t *testing.T) {
	_, _, s := testServer(t)
	ctx := context.Background()
	t.Setenv("TELEGRAM_TOKEN", "")
	if channelCmd(ctx, s.db, []string{"add", "telegram", "42"}) == nil {
		t.Fatal("telegram without a token should fail")
	}
	if channelCmd(ctx, s.db, []string{"add", "pigeon", "x"}) == nil {
		t.Fatal("unknown kind should fail")
	}
	t.Setenv("TELEGRAM_TOKEN", "123:abc")
	if err := channelCmd(ctx, s.db, []string{"add", "telegram", "42"}); err != nil {
		t.Fatal(err)
	}
	var cfg channelConfig
	s.db.QueryRow(ctx, `SELECT config FROM channels WHERE kind = 'telegram'`).Scan(&cfg)
	if cfg.Token != "123:abc" || cfg.Chat != "42" || cfg.URL != "" {
		t.Fatalf("stored: %+v", cfg)
	}
}
