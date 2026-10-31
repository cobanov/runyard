package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestS3Blobs runs against the S3-compatible server in RUNYARD_TEST_S3 (`make test` starts SeaweedFS for it).
func TestS3Blobs(t *testing.T) {
	ep := os.Getenv("RUNYARD_TEST_S3")
	if ep == "" {
		t.Skip("RUNYARD_TEST_S3 not set; run `make test`")
	}
	ctx := context.Background()
	root := &s3Blobs{endpoint: ep, bucket: "runyard-test", region: "us-east-1", id: "runyard", secret: "runyard-test-secret"}
	if res, err := root.do(ctx, "PUT", "", nil, 0, ""); err == nil {
		res.Body.Close()
	} else if !strings.Contains(err.Error(), "BucketAlready") {
		t.Fatal("create bucket: ", err)
	}
	b := *root
	b.prefix = "media/"
	key := "0b5c4a8e-1f2d-4c3b-9a8e-7d6c5b4a3f21/11111111-2222-4333-8444-555555555555.png"
	data := []byte("not really a png")
	if err := b.put(ctx, key, bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatal(err)
	}
	if got, err := readBlob(ctx, &b, key); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("read back %q, %v", got, err)
	}
	if err := b.put(ctx, key+".2", strings.NewReader("unknown length"), -1); err != nil { // an MLflow upload without Content-Length
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/media/"+key, nil)
	req.Header.Set("Range", "bytes=4-9")
	w := httptest.NewRecorder()
	b.serve(w, req, key)
	if w.Code != http.StatusPartialContent || w.Body.String() != "really" {
		t.Fatalf("range: %d %q", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	b.serve(w, httptest.NewRequest("GET", "/media/x", nil), "missing.png")
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing: %d", w.Code)
	}

	if err := b.remove(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := b.open(ctx, key); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("after remove: %v", err)
	}
	bad := b
	bad.secret = "wrong"
	if _, err := bad.open(ctx, key+".2"); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("a wrong secret should be refused: %v", err)
	}
	f, _ := b.open(ctx, key+".2")
	got, _ := io.ReadAll(f)
	f.Close()
	if string(got) != "unknown length" {
		t.Fatalf("unknown-length upload: %q", got)
	}
}
