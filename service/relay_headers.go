package service

import (
	"net/http"
	"strings"
)

// relayResponseHeaderAllowlist 列出可以安全中转给 API 客户端的上游响应头（端到端语义）。
// 只有出现在白名单里的头才会被复制；其余一律丢弃。
// relayResponseHeaderAllowlist lists the upstream response headers that are safe to relay
// to API clients (end-to-end semantics). Only headers in this list are copied.
var relayResponseHeaderAllowlist = canonicalHeaderSet(
	"Content-Type",
	"Content-Disposition",
	"Content-Length",
	"Content-Range",
	"Accept-Ranges",
	"ETag",
	"Last-Modified",
	"Cache-Control",
	"Expires",
)

// relayResponseHeaderDenylist 列出明确禁止透传的头（会话、安全策略、CORS、逐跳头等）。
// 即使将来有人扩充白名单，这里的头也不会被复制。
// relayResponseHeaderDenylist lists headers that must never be relayed (cookies, security
// policies, CORS, hop-by-hop). These are dropped even if the allowlist is extended later.
var relayResponseHeaderDenylist = canonicalHeaderSet(
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
)

// canonicalHeaderSet 把头名统一成 http.Header 的规范形式后放进集合，
// 这样 "ETag"/"Etag"、"TE"/"Te" 之类的写法差异不会导致漏判。
// canonicalHeaderSet normalizes header names to http.Header's canonical form so that
// spelling differences ("ETag" vs "Etag", "TE" vs "Te") cannot defeat the lookup.
func canonicalHeaderSet(names ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		set[http.CanonicalHeaderKey(name)] = struct{}{}
	}
	return set
}

// CopyRelayResponseHeaders 只把上游响应中白名单内的头复制到客户端响应。
// 头名按大小写不敏感匹配（http.Header 通常会做规范化，这里再做一次防御）。
// CopyRelayResponseHeaders copies only allowlisted headers from an upstream response to the
// client response. Header names are matched case-insensitively.
func CopyRelayResponseHeaders(dst http.Header, src http.Header) {
	if dst == nil || src == nil {
		return
	}
	// RFC 7230: Connection 头中列出的字段名同样是逐跳头，必须一并丢弃。
	hopByHop := hopByHopHeaderNames(src)
	for key, values := range src {
		name := http.CanonicalHeaderKey(key)
		if !isRelayResponseHeaderAllowed(name, hopByHop) {
			continue
		}
		for _, value := range values {
			dst.Add(name, value)
		}
	}
}

// isRelayResponseHeaderAllowed 判断某个响应头是否允许透传给客户端。
func isRelayResponseHeaderAllowed(name string, hopByHop map[string]struct{}) bool {
	name = http.CanonicalHeaderKey(name)
	if _, denied := relayResponseHeaderDenylist[name]; denied {
		return false
	}
	// Access-Control-Allow-* / Access-Control-Expose-Headers 等由本服务自己的 CORS 中间件决定。
	if strings.HasPrefix(name, "Access-Control-") {
		return false
	}
	if _, listed := hopByHop[name]; listed {
		return false
	}
	_, allowed := relayResponseHeaderAllowlist[name]
	return allowed
}

// hopByHopHeaderNames 解析 Connection 头中声明的逐跳头名。
func hopByHopHeaderNames(header http.Header) map[string]struct{} {
	names := make(map[string]struct{})
	for _, value := range header.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			names[http.CanonicalHeaderKey(token)] = struct{}{}
		}
	}
	return names
}
