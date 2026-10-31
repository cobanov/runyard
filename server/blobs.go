package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// blobs holds media and artifact files under keys like "<run id>/<file id>.png": a local directory (MEDIA_DIR)
// or an S3-compatible bucket such as Cloudflare R2 (S3_BUCKET and friends).
type blobs interface {
	put(ctx context.Context, key string, r io.Reader, size int64) error
	open(ctx context.Context, key string) (io.ReadCloser, error)
	remove(ctx context.Context, key string) error
	serve(w http.ResponseWriter, r *http.Request, key string) // with Range support, for video seeking
}

func newBlobs() (blobs, error) {
	bucket := os.Getenv("S3_BUCKET")
	if bucket == "" {
		dir := env("MEDIA_DIR", "./media")
		return localBlobs(dir), os.MkdirAll(dir, 0o755)
	}
	b := &s3Blobs{endpoint: strings.TrimRight(os.Getenv("S3_ENDPOINT"), "/"), bucket: bucket, region: env("S3_REGION", "auto"),
		prefix: os.Getenv("S3_PREFIX"), id: os.Getenv("S3_ACCESS_KEY_ID"), secret: os.Getenv("S3_SECRET_ACCESS_KEY")}
	if b.endpoint == "" || b.id == "" || b.secret == "" {
		return nil, errors.New("S3_BUCKET needs S3_ENDPOINT, S3_ACCESS_KEY_ID and S3_SECRET_ACCESS_KEY")
	}
	return b, nil
}

type localBlobs string

func (d localBlobs) path(key string) string { return filepath.Join(string(d), filepath.FromSlash(key)) }

func (d localBlobs) put(_ context.Context, key string, r io.Reader, _ int64) error {
	p := d.path(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(p)
	}
	return err
}

func (d localBlobs) open(_ context.Context, key string) (io.ReadCloser, error) { return os.Open(d.path(key)) }

func (d localBlobs) remove(_ context.Context, key string) error { return os.Remove(d.path(key)) }

func (d localBlobs) serve(w http.ResponseWriter, r *http.Request, key string) {
	f, err := os.Open(d.path(key))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}

// s3Blobs talks to an S3-compatible API with path-style URLs and SigV4 over an unsigned payload (fine over HTTPS).
// shortcut: a single PUT per object, so at most 5 GB each; add multipart uploads when checkpoints outgrow that.
type s3Blobs struct {
	endpoint, bucket, region, prefix, id, secret string
}

// do signs and sends one request. Keys are made of [0-9a-z./-] (ids and extensions), so they need no escaping.
func (b *s3Blobs) do(ctx context.Context, method, key string, body io.Reader, size int64, rng string) (*http.Response, error) {
	u, err := url.Parse(b.endpoint + "/" + b.bucket + "/" + b.prefix + key)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.ContentLength = size
	}
	if rng != "" {
		req.Header.Set("Range", rng)
	}
	now := time.Now().UTC()
	amzDate, day := now.Format("20060102T150405Z"), now.Format("20060102")
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
	canonical := strings.Join([]string{method, u.EscapedPath(), "",
		"host:" + u.Host, "x-amz-content-sha256:UNSIGNED-PAYLOAD", "x-amz-date:" + amzDate, "",
		"host;x-amz-content-sha256;x-amz-date", "UNSIGNED-PAYLOAD"}, "\n")
	scope := day + "/" + b.region + "/s3/aws4_request"
	sum := sha256.Sum256([]byte(canonical))
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(sum[:])
	k := []byte("AWS4" + b.secret)
	for _, part := range []string{day, b.region, "s3", "aws4_request", toSign} {
		m := hmac.New(sha256.New, k)
		m.Write([]byte(part))
		k = m.Sum(nil)
	}
	req.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature=%x",
		b.id, scope, k))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 && res.StatusCode != http.StatusNotModified {
		defer res.Body.Close()
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		if res.StatusCode == http.StatusNotFound {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("s3 %s %s: %s: %s", method, key, res.Status, msg)
	}
	return res, nil
}

func (b *s3Blobs) put(ctx context.Context, key string, r io.Reader, size int64) error {
	if size < 0 { // a signed request needs the length up front
		f, err := os.CreateTemp("", "runyard-upload")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		defer f.Close()
		if size, err = io.Copy(f, r); err != nil {
			return err
		}
		f.Seek(0, io.SeekStart)
		r = f
	}
	res, err := b.do(ctx, "PUT", key, r, size, "")
	if err != nil {
		return err
	}
	return res.Body.Close()
}

func (b *s3Blobs) open(ctx context.Context, key string) (io.ReadCloser, error) {
	res, err := b.do(ctx, "GET", key, nil, 0, "")
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

func (b *s3Blobs) remove(ctx context.Context, key string) error {
	res, err := b.do(ctx, "DELETE", key, nil, 0, "")
	if err != nil {
		return err
	}
	return res.Body.Close()
}

// serve streams the object through this server, so the bucket stays private and media URLs stay the same.
func (b *s3Blobs) serve(w http.ResponseWriter, r *http.Request, key string) {
	res, err := b.do(r.Context(), "GET", key, nil, 0, r.Header.Get("Range"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
		} else {
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
		return
	}
	defer res.Body.Close()
	for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if v := res.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(res.StatusCode)
	io.Copy(w, res.Body)
}

// readBlob reads a whole small file (an image for a notification, a text sample for MCP).
func readBlob(ctx context.Context, b blobs, key string) ([]byte, error) {
	f, err := b.open(ctx, key)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 64<<20))
}
