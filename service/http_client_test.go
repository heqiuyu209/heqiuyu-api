package service

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/heqiuyu/heqiuyu-api/setting/system_setting"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http/httpproxy"
)

type fetchRoundTripFunc func(*http.Request) (*http.Response, error)

func (f fetchRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func setFetchTestPolicy(t *testing.T) *system_setting.FetchSetting {
	t.Helper()
	setting := system_setting.GetFetchSetting()
	original := *setting
	t.Cleanup(func() { *setting = original })
	*setting = system_setting.FetchSetting{EnableSSRFProtection: true, ApplyIPFilterForDomain: true}
	return setting
}

func TestFetchTransportUsesEnvironmentProxyAndNoProxy(t *testing.T) {
	setFetchTestPolicy(t)
	var proxyRequests atomic.Int32
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		proxyRequests.Add(1)
		if req.URL.Host != "8.8.8.8" || req.URL.Path != "/through-proxy" {
			t.Errorf("unexpected proxy destination: %s", req.URL)
		}
		_, _ = io.WriteString(w, "proxy")
	}))
	defer proxyServer.Close()
	t.Setenv("HTTP_PROXY", proxyServer.URL)
	t.Setenv("HTTPS_PROXY", proxyServer.URL)
	t.Setenv("NO_PROXY", "8.8.4.4")
	t.Setenv("REQUEST_METHOD", "")
	// Avoid net/http's process-wide environment cache so this test is independent
	// of which earlier tests created an HTTP client.
	proxyFromEnvironment := httpproxy.FromEnvironment().ProxyFunc()
	selector := func(req *http.Request) (*url.URL, error) { return proxyFromEnvironment(req.URL) }
	transport := newFetchRoundTripper()
	defer transport.CloseIdleConnections()
	transport.proxyForRequest = selector
	transport.proxy.(*http.Transport).Proxy = selector
	directRequests := 0
	transport.direct = fetchRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		directRequests++
		require.Equal(t, "8.8.4.4", req.URL.Host)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("direct")), Header: make(http.Header), Request: req}, nil
	})
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	for _, target := range []struct{ url, body string }{
		{"http://8.8.8.8/through-proxy", "proxy"},
		{"http://8.8.4.4/direct", "direct"},
	} {
		response, err := client.Get(target.url)
		require.NoError(t, err)
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		require.NoError(t, err)
		require.Equal(t, target.body, string(body))
	}
	require.Equal(t, int32(1), proxyRequests.Load())
	require.Equal(t, 1, directRequests)
}

func TestFetchTransportRejectsPrivateDestinationBeforeProxy(t *testing.T) {
	setFetchTestPolicy(t)
	for _, useProxy := range []bool{false, true} {
		transport := newFetchRoundTripper()
		defer transport.CloseIdleConnections()
		transport.proxyForRequest = func(*http.Request) (*url.URL, error) {
			if useProxy {
				return url.Parse("http://127.0.0.1:8080")
			}
			return nil, nil
		}
		unexpectedRequest := fetchRoundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("private destination reached a transport")
			return nil, nil
		})
		transport.direct, transport.proxy = unexpectedRequest, unexpectedRequest
		req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/secret", nil)
		require.NoError(t, err)
		_, err = transport.RoundTrip(req)
		require.ErrorContains(t, err, "private IP address not allowed")
	}
}

func TestValidatedDialHonorsDomainIPFilterSetting(t *testing.T) {
	setting := setFetchTestPolicy(t)
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	require.NoError(t, err)
	for _, test := range []struct {
		name, host      string
		filter, allowed bool
	}{
		{"domain opted out", "localhost", false, true},
		{"domain protected", "localhost", true, false},
		{"literal still protected", "127.0.0.1", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			setting.ApplyIPFilterForDomain = test.filter
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, err := validatedDialContext(&net.Dialer{})(ctx, "tcp", net.JoinHostPort(test.host, port))
			if test.allowed {
				require.NoError(t, err)
				_ = conn.Close()
			} else {
				require.ErrorContains(t, err, "private IP address not allowed")
				require.Nil(t, conn)
			}
		})
	}
}
