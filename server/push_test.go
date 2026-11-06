package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func unb64(t *testing.T, s string) []byte {
	b, err := b64.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestEncryptPushRFC8291 checks the encryption against the worked example in RFC 8291, Appendix A.
func TestEncryptPushRFC8291(t *testing.T) {
	as, err := ecdh.P256().NewPrivateKey(unb64(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := encryptPush([]byte("When I grow up, I want to be a watermelon"),
		unb64(t, "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"),
		unb64(t, "BTBZMqHH6r4Tts7J_aSIgg"), unb64(t, "DGv6ra1nlYgDCS1FRnbzlw"), as)
	if err != nil {
		t.Fatal(err)
	}
	want := "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
	if b64.EncodeToString(got) != want {
		t.Fatalf("got  %s\nwant %s", b64.EncodeToString(got), want)
	}
}

// decryptPush is the browser's side, for the fake push service below.
func decryptPush(t *testing.T, body []byte, ua *ecdh.PrivateKey, auth []byte) []byte {
	salt, idlen := body[:16], int(body[20])
	asPub, ct := body[21:21+idlen], body[21+idlen:]
	pub, _ := ecdh.P256().NewPublicKey(asPub)
	secret, _ := ua.ECDH(pub)
	ikm, _ := hkdf.Key(sha256.New, secret, auth, "WebPush: info\x00"+string(ua.PublicKey().Bytes())+string(asPub), 32)
	cek, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil || plain[len(plain)-1] != 2 {
		t.Fatalf("decrypt: %v", err)
	}
	return plain[:len(plain)-1]
}

func TestWebPush(t *testing.T) {
	ts, key, s := testServer(t)
	ctx := context.Background()
	var pk struct{ Key string }
	call(t, "GET", ts.URL+"/api/v1/push/key", "", nil, &pk)
	pubBytes := unb64(t, pk.Key)

	// Only push services are accepted as endpoints, so subscribing cannot point the server elsewhere.
	sub := map[string]any{"endpoint": "https://fcm.googleapis.com/fcm/send/abc", "keys": map[string]string{"p256dh": pk.Key, "auth": "BTBZMqHH6r4Tts7J_aSIgg"}}
	for _, bad := range []string{"http://fcm.googleapis.com/x", "https://127.0.0.1/x", "https://evil.example/fcm.googleapis.com"} {
		if code := call(t, "POST", ts.URL+"/api/v1/push/subscribe", "", map[string]any{"endpoint": bad, "keys": sub["keys"]}, nil); code != 400 {
			t.Fatalf("%s: %d", bad, code)
		}
	}
	var first, again struct{ Channel int64 }
	call(t, "POST", ts.URL+"/api/v1/push/subscribe", "", sub, &first)
	call(t, "POST", ts.URL+"/api/v1/push/subscribe", "", sub, &again)
	if first.Channel == 0 || again.Channel != first.Channel {
		t.Fatalf("subscribing twice: %d, %d", first.Channel, again.Channel)
	}
	call(t, "POST", ts.URL+"/api/v1/push/unsubscribe", "", sub, nil)

	// A fake push service: checks the VAPID token, decrypts like a browser, then answers 410 the second time.
	ua, _ := ecdh.P256().GenerateKey(nil)
	auth := []byte("0123456789abcdef")
	got := make(chan map[string]string, 2)
	var calls int
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls > 1 {
			w.WriteHeader(http.StatusGone)
			return
		}
		tok, k, _ := strings.Cut(strings.TrimPrefix(r.Header.Get("Authorization"), "vapid t="), ", k=")
		if k != pk.Key || r.Header.Get("Content-Encoding") != "aes128gcm" || r.Header.Get("TTL") == "" {
			t.Errorf("headers: %v", r.Header)
		}
		parts := strings.Split(tok, ".")
		x, y := new(big.Int).SetBytes(pubBytes[1:33]), new(big.Int).SetBytes(pubBytes[33:])
		pub := &ecdsa.PublicKey{Curve: vapid.Curve, X: x, Y: y}
		sig := unb64(t, parts[2])
		h := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
		var claims map[string]any
		json.Unmarshal(unb64(t, parts[1]), &claims)
		if !ecdsa.Verify(pub, h[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) || claims["aud"] != "http://"+r.Host {
			t.Errorf("bad VAPID token: %v", claims)
		}
		body, _ := io.ReadAll(r.Body)
		var msg map[string]string
		json.Unmarshal(decryptPush(t, body, ua, auth), &msg)
		got <- msg
		w.WriteHeader(http.StatusCreated)
	}))
	defer fake.Close()
	cfg, _ := json.Marshal(channelConfig{URL: fake.URL + "/push/1", P256dh: b64.EncodeToString(ua.PublicKey().Bytes()), Auth: b64.EncodeToString(auth)})
	var ch int64
	s.db.QueryRow(ctx, `INSERT INTO channels (kind, config) VALUES ('webpush', $1) RETURNING id`, cfg).Scan(&ch)
	s.db.Exec(ctx, `INSERT INTO rules (channel_id, event) VALUES ($1, 'finished')`, ch)

	finish := func(name string) {
		id := newUUID()
		call(t, "POST", ts.URL+"/api/v1/runs", key, map[string]any{"id": id, "project": "push", "name": name}, nil)
		call(t, "PATCH", ts.URL+"/api/v1/runs/"+id, key, map[string]any{"status": "finished"}, nil)
	}
	finish("one")
	select {
	case m := <-got:
		if m["title"] != "push/one: Finished" || !strings.HasPrefix(m["body"], "Ran for") {
			t.Fatal(m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no push arrived")
	}
	finish("two") // the service answers 410: the subscription is deleted
	for end := time.Now().Add(5 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		var n int
		s.db.QueryRow(ctx, `SELECT count(*) FROM channels WHERE id = $1`, ch).Scan(&n)
		if n == 0 {
			break
		}
		if time.Now().After(end) {
			t.Fatal("gone subscription was kept")
		}
	}
}
