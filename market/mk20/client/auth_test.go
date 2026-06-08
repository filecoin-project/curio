package client

import (
	"bytes"
	"crypto/sha256"
	"testing"
	"time"
)

func TestAuthDigest(t *testing.T) {
	pubKey := []byte{1, 2, 3, 4}

	ts, err := time.Parse(time.RFC3339, "2026-06-08T05:10:36Z")
	if err != nil {
		t.Fatal(err)
	}

	got := AuthDigest(pubKey, "post", "/market/mk20/deal", ts)

	want := sha256.Sum256(bytes.Join([][]byte{
		pubKey,
		[]byte("POST"),
		[]byte("/market/mk20/deal"),
		[]byte("2026-06-08T05:10:00Z"),
	}, []byte{}))

	if got != want {
		t.Fatalf("unexpected digest: got %x want %x", got, want)
	}
}

func TestAuthDigestEmptyPath(t *testing.T) {
	pubKey := []byte{1, 2, 3, 4}

	ts, err := time.Parse(time.RFC3339, "2026-06-08T05:10:36Z")
	if err != nil {
		t.Fatal(err)
	}

	got := AuthDigest(pubKey, "GET", "", ts)

	want := sha256.Sum256(bytes.Join([][]byte{
		pubKey,
		[]byte("GET"),
		[]byte("/"),
		[]byte("2026-06-08T05:10:00Z"),
	}, []byte{}))

	if got != want {
		t.Fatalf("unexpected digest: got %x want %x", got, want)
	}
}
