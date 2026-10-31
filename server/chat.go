package main

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"strings"
)

// text is the message as plain lines: title, body, link.
func (m message) text() string {
	s := m.Title + "\n" + m.Body
	if m.URL != "" {
		s += "\n" + m.URL
	}
	return s
}

func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// sendSlack posts to an incoming webhook. Slack fetches images itself, so the sample shows only when RUNYARD_URL is public.
func sendSlack(ctx context.Context, hook string, m message) error {
	esc := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	text := "*" + esc.Replace(m.Title) + "*\n" + esc.Replace(m.Body)
	if m.URL != "" {
		text += "\n<" + m.URL + "|Open the run>"
	}
	b, _ := json.Marshal(map[string]string{"text": cut(text, 3000)})
	return post(ctx, "POST", hook, "", bytes.NewReader(b), "application/json")
}

// sendDiscord posts to a channel webhook, with the latest sample attached.
func sendDiscord(ctx context.Context, hook string, m message) error {
	payload, _ := json.Marshal(map[string]string{"content": cut("**"+m.Title+"**\n"+m.Body+linkLine(m), 2000)})
	if m.image == nil {
		return post(ctx, "POST", hook, "", bytes.NewReader(payload), "application/json")
	}
	body, ctype := form(map[string]string{"payload_json": string(payload)}, "files[0]", "sample"+m.imageExt, m.image)
	return post(ctx, "POST", hook, "", body, ctype)
}

func linkLine(m message) string {
	if m.URL == "" {
		return ""
	}
	return "\n" + m.URL
}

// sendTelegram sends through the Bot API: the sample as a photo with the text as caption, or a plain message.
func sendTelegram(ctx context.Context, api, token, chat string, m message) error {
	if api == "" {
		api = "https://api.telegram.org"
	}
	base := api + "/bot" + token + "/"
	if m.image != nil {
		body, ctype := form(map[string]string{"chat_id": chat, "caption": cut(m.text(), 1024)}, "photo", "sample"+m.imageExt, m.image)
		if err := post(ctx, "POST", base+"sendPhoto", "", body, ctype); err == nil {
			return nil
		}
	}
	b, _ := json.Marshal(map[string]any{"chat_id": chat, "text": cut(m.text(), 4096), "disable_web_page_preview": true})
	return post(ctx, "POST", base+"sendMessage", "", bytes.NewReader(b), "application/json")
}

func form(fields map[string]string, fileField, filename string, data []byte) (*bytes.Buffer, string) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		w.WriteField(k, v)
	}
	f, _ := w.CreateFormFile(fileField, filename)
	f.Write(data)
	w.Close()
	return &buf, w.FormDataContentType()
}
