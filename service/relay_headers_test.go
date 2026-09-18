package service

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCopyRelayResponseHeaders(t *testing.T) {
	allowed := []string{
		"Content-Type",
		"Content-Encoding",
		"Content-Disposition",
		"Content-Length",
		"Content-Range",
		"Accept-Ranges",
		"ETag",
		"Last-Modified",
		"Cache-Control",
		"Expires",
	}
	denied := []string{
		"Set-Cookie",
		"Set-Cookie2",
		"Content-Security-Policy",
		"Content-Security-Policy-Report-Only",
		"X-Frame-Options",
		"Strict-Transport-Security",
		"Server",
		"Via",
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"TE",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
		"Access-Control-Allow-Origin",
		"Access-Control-Expose-Headers",
		"X-Upstream-Trace",
	}

	src := make(http.Header, len(allowed)+len(denied))
	for _, name := range allowed {
		src.Set(name, "allowed-"+name)
	}
	for _, name := range denied {
		src.Set(name, "denied-"+name)
	}

	dst := http.Header{}
	CopyRelayResponseHeaders(dst, src)

	for _, name := range allowed {
		if got := dst.Get(name); got != "allowed-"+name {
			t.Errorf("header %q should be copied to the client, got %q", name, got)
		}
	}
	for _, name := range denied {
		if got := dst.Get(name); got != "" {
			t.Errorf("header %q must not be copied to the client, got %q", name, got)
		}
	}
}

func TestCopyRelayResponseHeadersDropsConnectionListedHeaders(t *testing.T) {
	src := http.Header{
		"Connection":   {"close, X-Custom-Hop"},
		"X-Custom-Hop": {"should-be-dropped"},
		"Content-Type": {"video/mp4"},
	}

	dst := http.Header{}
	CopyRelayResponseHeaders(dst, src)

	if got := dst.Get("X-Custom-Hop"); got != "" {
		t.Errorf("Connection-listed hop-by-hop header must not be copied, got %q", got)
	}
	if got := dst.Get("Content-Type"); got != "video/mp4" {
		t.Errorf("Content-Type should be copied, got %q", got)
	}
}

func TestCopyRelayResponseHeadersMatchesNamesCaseInsensitively(t *testing.T) {
	src := http.Header{
		"content-TYPE": {"audio/mpeg"},
		"set-cookie":   {"session=1"},
		"ETAG":         {`"v1"`},
	}

	dst := http.Header{}
	CopyRelayResponseHeaders(dst, src)

	if got := dst.Get("Content-Type"); got != "audio/mpeg" {
		t.Errorf("Content-Type should be copied despite odd casing, got %q", got)
	}
	if got := dst.Get("ETag"); got != `"v1"` {
		t.Errorf("ETag should be copied despite odd casing, got %q", got)
	}
	if got := dst.Get("Set-Cookie"); got != "" {
		t.Errorf("Set-Cookie must not be copied despite odd casing, got %q", got)
	}
}

func TestRelayPreservesCompressedBodyEncoding(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write([]byte("audio content")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	CopyRelayResponseHeaders(response.Header(), http.Header{
		"Content-Type":     {"audio/mpeg"},
		"Content-Encoding": {"gzip"},
	})
	if _, err := io.Copy(response, &compressed); err != nil {
		t.Fatal(err)
	}
	if response.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("compressed media must retain its content encoding")
	}
	reader, err := gzip.NewReader(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil || string(body) != "audio content" {
		t.Fatalf("decode relayed media: body=%q, err=%v", body, err)
	}
}

func TestCopyRelayResponseHeadersDropsMixedCaseConnectionOptions(t *testing.T) {
	dst := http.Header{}
	CopyRelayResponseHeaders(dst, http.Header{
		"connection":       {"Content-Encoding, ETag"},
		"Content-Encoding": {"gzip"},
		"ETag":             {`"upstream"`},
	})
	if dst.Get("Content-Encoding") != "" || dst.Get("ETag") != "" {
		t.Fatalf("connection-specific headers leaked: %v", dst)
	}
}
