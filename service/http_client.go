package service

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/setting/system_setting"

	"golang.org/x/net/proxy"
)

var (
	httpClient      *http.Client
	fetchClient     *http.Client
	proxyClientLock sync.Mutex
	proxyClients    = make(map[string]*http.Client)
)

// validatedDialContext 返回一个在真正建立连接前逐 IP 执行 SSRF 策略的 DialContext。
//
// 为什么需要它：ValidateURLWithFetchSetting 只在请求发起前解析一次域名，之后
// Transport 会自己再解析一次。攻击者控制的域名可以先返回公网地址通过校验，再在
// 拨号时返回 127.0.0.1 / 169.254.169.254，从而绕过全部防护（DNS 重绑定 TOCTOU，
// 审计报告 M3）。这里把策略下沉到拨号阶段，只连接通过校验的 IP。
func validatedDialContext(base *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}

		fetchSetting := system_setting.GetFetchSetting()
		// This opt-out applies only to domain names; literal IPs still use IP policy.
		if net.ParseIP(host) == nil && !fetchSetting.ApplyIPFilterForDomain {
			return base.DialContext(ctx, network, addr)
		}

		var candidates []net.IP
		if ip := net.ParseIP(host); ip != nil {
			candidates = append(candidates, ip)
		} else {
			resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, item := range resolved {
				candidates = append(candidates, item.IP)
			}
		}

		var lastErr error
		for _, ip := range candidates {
			if err := common.ValidateIPWithFetchSetting(ip, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.IpFilterMode, fetchSetting.IpList); err != nil {
				lastErr = err
				continue
			}
			conn, dialErr := base.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("no usable address for host %s", host)
		}
		return nil, lastErr
	}
}

// newFetchTransport 构造带连接期 SSRF 校验的 Transport，供"处理用户可影响 URL"的客户端使用。
func newFetchTransport() *http.Transport {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		MaxIdleConns:        common.RelayMaxIdleConns,
		MaxIdleConnsPerHost: common.RelayMaxIdleConnsPerHost,
		ForceAttemptHTTP2:   true,
		DialContext:         validatedDialContext(dialer),
	}
	if common.TLSInsecureSkipVerify {
		transport.TLSClientConfig = common.InsecureTLSConfig
	}
	return transport
}

// Environment proxies are configured by the operator, just like explicit channel
// proxies. Validate the destination URL, not the proxy's address. DNS resolution
// beyond a proxy is governed by that trusted proxy; direct connections retain
// connection-time IP validation.
type fetchTransport struct {
	direct          http.RoundTripper
	proxy           http.RoundTripper
	proxyForRequest func(*http.Request) (*url.URL, error)
}

func newFetchRoundTripper() *fetchTransport {
	direct := newFetchTransport()
	proxied := direct.Clone()
	proxied.Proxy = http.ProxyFromEnvironment
	proxied.DialContext = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	return &fetchTransport{direct: direct, proxy: proxied, proxyForRequest: http.ProxyFromEnvironment}
}

func (t *fetchTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	setting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(req.URL.String(), setting.EnableSSRFProtection, setting.AllowPrivateIp, setting.DomainFilterMode, setting.IpFilterMode, setting.DomainList, setting.IpList, setting.AllowedPorts, setting.ApplyIPFilterForDomain); err != nil {
		return nil, fmt.Errorf("request reject: %w", err)
	}
	proxyURL, err := t.proxyForRequest(req)
	if err != nil {
		return nil, err
	}
	if proxyURL != nil {
		return t.proxy.RoundTrip(req)
	}
	return t.direct.RoundTrip(req)
}

func (t *fetchTransport) CloseIdleConnections() {
	for _, transport := range []http.RoundTripper{t.direct, t.proxy} {
		if closer, ok := transport.(interface{ CloseIdleConnections() }); ok {
			closer.CloseIdleConnections()
		}
	}
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	fetchSetting := system_setting.GetFetchSetting()
	urlStr := req.URL.String()
	if err := common.ValidateURLWithFetchSetting(urlStr, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		return fmt.Errorf("redirect to %s blocked: %v", urlStr, err)
	}
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	return nil
}

// CheckRedirectPolicy 是出站请求统一的重定向策略：逐跳复查 SSRF 策略并限制跳数。
//
// 供那些必须自带 Transport 的调用点复用（例如需要自定义拨号行为的抓取），避免它们
// 各自实现、或者干脆忘记实现重定向复查——裸 http.Client 会无条件跟随最多 10 跳重定向，
// 从而绕过请求前的 URL 校验。
func CheckRedirectPolicy(req *http.Request, via []*http.Request) error {
	return checkRedirect(req, via)
}

func InitHttpClient() {
	transport := &http.Transport{
		MaxIdleConns:        common.RelayMaxIdleConns,
		MaxIdleConnsPerHost: common.RelayMaxIdleConnsPerHost,
		ForceAttemptHTTP2:   true,
		Proxy:               http.ProxyFromEnvironment, // Support HTTP_PROXY, HTTPS_PROXY, NO_PROXY env vars
	}
	if common.TLSInsecureSkipVerify {
		transport.TLSClientConfig = common.InsecureTLSConfig
	}

	httpClient = &http.Client{
		Transport:     transport,
		Timeout:       effectiveRelayTimeout(),
		CheckRedirect: checkRedirect,
	}

	// 处理"用户可影响的 URL"（聊天中的图片/文件链接、Webhook、通知、视频代理、
	// MJ 图片代理、Worker、管理端抓取等）一律使用该客户端：它同时具备重定向复查与
	// 连接期 IP 校验。中继路径继续使用 httpClient —— 渠道 base_url 是运营者配置的，
	// 自建内网网关是正常用法，对其施加私网拦截会直接打断部署（见 CHANNEL_BASE_URL_STRICT）。
	fetchClient = &http.Client{
		Transport:     newFetchRoundTripper(),
		Timeout:       effectiveRelayTimeout(),
		CheckRedirect: checkRedirect,
	}
}

// effectiveRelayTimeout 返回实际生效的 relay 上游超时时间。
// 当 RELAY_TIMEOUT 未配置或为 0 时，使用兜底 300 秒，避免创建无超时的 http.Client。
func effectiveRelayTimeout() time.Duration {
	if common.RelayTimeout <= 0 {
		return 300 * time.Second
	}
	return time.Duration(common.RelayTimeout) * time.Second
}

func GetHttpClient() *http.Client {
	return httpClient
}

// GetFetchClient 返回用于"用户可影响的 URL"的客户端：
// 每次请求/重定向都校验目标 URL；直连时另做连接期 IP 校验（防 DNS 重绑定）。
// 环境变量配置的代理视为受信基础设施，代理后的 DNS/网络策略由代理负责。
// 所有下载、Webhook、通知、代理类抓取都应使用它，而不是 GetHttpClient。
func GetFetchClient() *http.Client {
	if fetchClient == nil {
		// InitHttpClient 尚未调用（例如测试）：退回默认客户端，保持可用性。
		return httpClient
	}
	return fetchClient
}

// DoOutboundRequest 发起一次对外请求：先按 fetch 策略校验目标 URL，再用策略感知的
// 客户端发送。所有处理用户可影响 URL 的调用点都应经由此函数，而不是直接 new
// 一个裸 http.Client（裸客户端既不做 URL 校验，也不复查重定向）。
func DoOutboundRequest(ctx context.Context, method string, rawURL string, headers map[string]string, body io.Reader) (*http.Response, error) {
	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(rawURL, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		return nil, fmt.Errorf("request reject: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	return GetFetchClient().Do(req)
}

// GetHttpClientWithProxy returns the default client or a proxy-enabled one when proxyURL is provided.
func GetHttpClientWithProxy(proxyURL string) (*http.Client, error) {
	if proxyURL == "" {
		return GetHttpClient(), nil
	}
	return NewProxyHttpClient(proxyURL)
}

// ResetProxyClientCache 清空代理客户端缓存，确保下次使用时重新初始化
func ResetProxyClientCache() {
	proxyClientLock.Lock()
	defer proxyClientLock.Unlock()
	for _, client := range proxyClients {
		if transport, ok := client.Transport.(*http.Transport); ok && transport != nil {
			transport.CloseIdleConnections()
		}
	}
	proxyClients = make(map[string]*http.Client)
}

// NewProxyHttpClient 创建支持代理的 HTTP 客户端
func NewProxyHttpClient(proxyURL string) (*http.Client, error) {
	if proxyURL == "" {
		if client := GetHttpClient(); client != nil {
			return client, nil
		}
		return http.DefaultClient, nil
	}

	proxyClientLock.Lock()
	if client, ok := proxyClients[proxyURL]; ok {
		proxyClientLock.Unlock()
		return client, nil
	}
	proxyClientLock.Unlock()

	parsedURL, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}

	switch parsedURL.Scheme {
	case "http", "https":
		transport := &http.Transport{
			MaxIdleConns:        common.RelayMaxIdleConns,
			MaxIdleConnsPerHost: common.RelayMaxIdleConnsPerHost,
			ForceAttemptHTTP2:   true,
			Proxy:               http.ProxyURL(parsedURL),
		}
		if common.TLSInsecureSkipVerify {
			transport.TLSClientConfig = common.InsecureTLSConfig
		}
		client := &http.Client{
			Transport:     transport,
			CheckRedirect: checkRedirect,
		}
		client.Timeout = effectiveRelayTimeout()
		proxyClientLock.Lock()
		proxyClients[proxyURL] = client
		proxyClientLock.Unlock()
		return client, nil

	case "socks5", "socks5h":
		// 获取认证信息
		var auth *proxy.Auth
		if parsedURL.User != nil {
			auth = &proxy.Auth{
				User:     parsedURL.User.Username(),
				Password: "",
			}
			if password, ok := parsedURL.User.Password(); ok {
				auth.Password = password
			}
		}

		// 创建 SOCKS5 代理拨号器
		// proxy.SOCKS5 使用 tcp 参数，所有 TCP 连接包括 DNS 查询都将通过代理进行。行为与 socks5h 相同
		dialer, err := proxy.SOCKS5("tcp", parsedURL.Host, auth, proxy.Direct)
		if err != nil {
			return nil, err
		}

		transport := &http.Transport{
			MaxIdleConns:        common.RelayMaxIdleConns,
			MaxIdleConnsPerHost: common.RelayMaxIdleConnsPerHost,
			ForceAttemptHTTP2:   true,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			},
		}
		if common.TLSInsecureSkipVerify {
			transport.TLSClientConfig = common.InsecureTLSConfig
		}

		client := &http.Client{Transport: transport, CheckRedirect: checkRedirect}
		client.Timeout = effectiveRelayTimeout()
		proxyClientLock.Lock()
		proxyClients[proxyURL] = client
		proxyClientLock.Unlock()
		return client, nil

	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s, must be http, https, socks5 or socks5h", parsedURL.Scheme)
	}
}
