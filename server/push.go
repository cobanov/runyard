package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Web push to the installed web app (RFC 8030): the browser subscribes with a push service (Google, Mozilla, Apple),
// the server sends there, encrypted for the browser (RFC 8291) and signed with the server's VAPID key (RFC 8292).

var b64 = base64.RawURLEncoding

// vapid is the server's push key pair, made on first start and kept in settings, so subscriptions survive restarts.
var vapid *ecdsa.PrivateKey

func loadVAPID(ctx context.Context, db *pgxpool.Pool) error {
	var v string
	err := db.QueryRow(ctx, `SELECT value FROM settings WHERE key = 'vapid'`).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		der, _ := x509.MarshalPKCS8PrivateKey(k)
		// two servers starting at once: the first key wins, both read it back
		if _, err := db.Exec(ctx, `INSERT INTO settings (key, value) VALUES ('vapid', $1) ON CONFLICT DO NOTHING`, b64.EncodeToString(der)); err != nil {
			return err
		}
		return loadVAPID(ctx, db)
	}
	if err != nil {
		return err
	}
	der, err := b64.DecodeString(v)
	if err != nil {
		return fmt.Errorf("settings vapid: %w", err)
	}
	k, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return fmt.Errorf("settings vapid: %w", err)
	}
	vapid = k.(*ecdsa.PrivateKey)
	return nil
}

func vapidPublic() string {
	pub, _ := vapid.PublicKey.ECDH()
	return b64.EncodeToString(pub.Bytes())
}

// encryptPush is RFC 8291: ECDH with the browser's key and HKDF with its auth secret give the key for one
// aes128gcm record (RFC 8188) whose header carries the salt and the sender's public key.
func encryptPush(plain, uaPublic, authSecret, salt []byte, as *ecdh.PrivateKey) ([]byte, error) {
	ua, err := ecdh.P256().NewPublicKey(uaPublic)
	if err != nil {
		return nil, fmt.Errorf("browser key: %w", err)
	}
	secret, err := as.ECDH(ua)
	if err != nil {
		return nil, err
	}
	asPub := as.PublicKey().Bytes()
	ikm, err := hkdf.Key(sha256.New, secret, authSecret, "WebPush: info\x00"+string(uaPublic)+string(asPub), 32)
	if err != nil {
		return nil, err
	}
	cek, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	header := append(slices.Clone(salt), 0, 0, 0x10, 0, byte(len(asPub))) // record size 4096, key id length
	header = append(header, asPub...)
	return gcm.Seal(header, nonce, append(slices.Clone(plain), 2), nil), nil // 2: the last (only) record
}

// vapidJWT signs a token for the push service at endpoint's origin, valid for 12 hours (the limit is 24).
func vapidJWT(endpoint, contact string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	claims, _ := json.Marshal(map[string]any{"aud": u.Scheme + "://" + u.Host, "exp": time.Now().Add(12 * time.Hour).Unix(), "sub": contact})
	signing := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`)) + "." + b64.EncodeToString(claims)
	h := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, vapid, h[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signing + "." + b64.EncodeToString(sig), nil
}

// errGone: the push service dropped the subscription (the app was uninstalled, permission revoked); delete the channel.
var errGone = errors.New("push subscription is gone")

func sendPush(ctx context.Context, c channelConfig, m message) error {
	if vapid == nil {
		return errors.New("no VAPID key loaded")
	}
	ua, err := b64.DecodeString(strings.TrimRight(c.P256dh, "="))
	if err != nil {
		return fmt.Errorf("p256dh: %w", err)
	}
	auth, err := b64.DecodeString(strings.TrimRight(c.Auth, "="))
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	// the whole payload must fit one 4096-byte record
	// the link is relative: the service worker belongs to this server, whatever address the phone reaches it by
	link := "/"
	if m.Run.ID != "" {
		link = "/#/r/" + m.Run.ID
	}
	payload, _ := json.Marshal(map[string]string{"title": cut(m.Title, 200), "body": cut(m.Body, 1000), "url": link, "tag": m.Run.ID + "/" + m.Event})
	as, _ := ecdh.P256().GenerateKey(rand.Reader)
	salt := make([]byte, 16)
	rand.Read(salt)
	body, err := encryptPush(payload, ua, auth, salt, as)
	if err != nil {
		return err
	}
	contact := env("RUNYARD_PUSH_CONTACT", "https://github.com/cobanov/runyard") // push services want a mailto: or https: URL
	jwt, err := vapidJWT(c.URL, contact)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "vapid t="+jwt+", k="+vapidPublic())
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", "86400")
	if m.priority == "high" {
		req.Header.Set("Urgency", "high")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone {
		return errGone
	}
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		return fmt.Errorf("push service: %s: %s", res.Status, b)
	}
	return nil
}

// pushHosts are the push services browsers use. The server only sends there, so a subscription cannot make it
// POST to an arbitrary address.
var pushHosts = []string{".googleapis.com", ".mozilla.com", ".mozaws.net", ".push.apple.com", ".notify.windows.com"}

type subscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

func (s *srv) pushRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/push/key", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"key": vapidPublic()})
	})
	// Subscribing needs no API key, like reading: a subscriber gets told what it could already read.
	mux.HandleFunc("POST /api/v1/push/subscribe", s.read(func(r *http.Request) (any, error) {
		var sub subscription
		if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&sub); err != nil {
			return nil, badRequest{err}
		}
		u, err := url.Parse(sub.Endpoint)
		if err != nil || u.Scheme != "https" || !slices.ContainsFunc(pushHosts, func(h string) bool { return strings.HasSuffix(u.Hostname(), h) }) {
			return nil, badRequest{errors.New("not a known push service endpoint")}
		}
		if _, err := b64.DecodeString(strings.TrimRight(sub.Keys.P256dh, "=")); err != nil || sub.Keys.Auth == "" {
			return nil, badRequest{errors.New("subscription keys missing")}
		}
		cfg, _ := json.Marshal(channelConfig{URL: sub.Endpoint, P256dh: sub.Keys.P256dh, Auth: sub.Keys.Auth})
		tx, err := s.db.Begin(r.Context())
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(r.Context())
		var id int64
		err = tx.QueryRow(r.Context(), `SELECT id FROM channels WHERE kind = 'webpush' AND config->>'url' = $1`, sub.Endpoint).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			var user *int64 // logged in: the subscription gets only what this user may see
			if p := principalOf(r.Context()); p != nil && p.user != 0 {
				user = &p.user
			}
			if err := tx.QueryRow(r.Context(), `INSERT INTO channels (kind, config, user_id) VALUES ('webpush', $1, $2) RETURNING id`, cfg, user).Scan(&id); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(r.Context(), `INSERT INTO rules (channel_id, event) SELECT $1, unnest($2::text[])`,
				id, []string{"finished", "failed", "crashed", "alert"}); err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}
		return map[string]int64{"channel": id}, tx.Commit(r.Context())
	}))
	mux.HandleFunc("POST /api/v1/push/unsubscribe", s.read(func(r *http.Request) (any, error) {
		var sub subscription
		if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&sub); err != nil {
			return nil, badRequest{err}
		}
		_, err := s.db.Exec(r.Context(), `DELETE FROM channels WHERE kind = 'webpush' AND config->>'url' = $1`, sub.Endpoint)
		return struct{}{}, err
	}))
}
