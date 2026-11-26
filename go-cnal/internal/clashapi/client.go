// Package clashapi 提供仅本机回环的 Mihomo/Clash fake-ip 缓存刷新。
package clashapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	secret  string
	http    *http.Client
}

// New 构造本机回环控制器客户端。空 URL 表示未启用，返回 nil。
func New(rawURL, secret string) (*Client, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, nil
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Host == "" {
		return nil, errors.New("clash_api_url 仅允许无凭据、无查询串的本机回环 HTTP 地址")
	}
	host := strings.ToLower(u.Hostname())
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return nil, fmt.Errorf("clash_api_url 必须指向本机回环，实际 %q", host)
		}
	}
	tr := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			if strings.EqualFold(host, "localhost") {
				host = "127.0.0.1"
			}
			return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(host, port))
		},
	}
	return &Client{
		baseURL: strings.TrimRight(u.String(), "/"),
		secret:  secret,
		http:    &http.Client{Transport: tr, Timeout: 2 * time.Second},
	}, nil
}

// FlushFakeIP 调用 Mihomo 标准接口 POST /cache/fakeip/flush。
func (c *Client) FlushFakeIP(ctx context.Context) error {
	if c == nil {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/cache/fakeip/flush", nil)
	if err != nil {
		return err
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("Clash fake-ip flush 返回 HTTP %d", resp.StatusCode)
	}
	return nil
}
