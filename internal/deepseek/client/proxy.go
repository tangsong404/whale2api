package client

import (
	"context"
	dsprotocol "whale2api/internal/deepseek/protocol"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"

	"whale2api/internal/auth"
	"whale2api/internal/config"
	trans "whale2api/internal/deepseek/transport"
)

type requestClients struct {
	regular   trans.Doer
	stream    trans.Doer
	fallback  *http.Client
	fallbackS *http.Client
}

type hostLookupFunc func(ctx context.Context, network, host string) ([]string, error)

var proxyConnectivityTestURL = "https://chat.deepseek.com/"

var defaultHostLookup hostLookupFunc = func(ctx context.Context, _ string, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}

func proxyDialAddress(ctx context.Context, proxyType, address string, lookup hostLookupFunc) (string, error) {
	proxyType = strings.ToLower(strings.TrimSpace(proxyType))
	if proxyType != "socks5" {
		return address, nil
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", err
	}
	if net.ParseIP(host) != nil {
		return address, nil
	}
	if lookup == nil {
		lookup = defaultHostLookup
	}
	addrs, err := lookup(ctx, "ip", host)
	if err != nil {
		return "", err
	}
	if len(addrs) == 0 {
		return "", fmt.Errorf("no ip address resolved for %s", host)
	}
	return net.JoinHostPort(addrs[0], port), nil
}

func proxyCacheKey(proxyCfg config.Proxy) string {
	proxyCfg = config.NormalizeProxy(proxyCfg)
	return strings.Join([]string{
		proxyCfg.ID,
		proxyCfg.Type,
		strings.ToLower(proxyCfg.Host),
		strconv.Itoa(proxyCfg.Port),
		proxyCfg.Username,
		proxyCfg.Password,
	}, "|")
}

func proxyDialContext(proxyCfg config.Proxy) (trans.DialContextFunc, error) {
	proxyCfg = config.NormalizeProxy(proxyCfg)
	var authCfg *proxy.Auth
	if proxyCfg.Username != "" || proxyCfg.Password != "" {
		authCfg = &proxy.Auth{User: proxyCfg.Username, Password: proxyCfg.Password}
	}
	forward := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	dialer, err := proxy.SOCKS5("tcp", net.JoinHostPort(proxyCfg.Host, strconv.Itoa(proxyCfg.Port)), authCfg, forward)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		target, err := proxyDialAddress(ctx, proxyCfg.Type, address, defaultHostLookup)
		if err != nil {
			return nil, err
		}
		if ctxDialer, ok := dialer.(proxy.ContextDialer); ok {
			return ctxDialer.DialContext(ctx, network, target)
		}
		return dialer.Dial(network, target)
	}, nil
}

func (c *Client) defaultRequestClients() requestClients {
	return requestClients{
		regular:   c.regular,
		stream:    c.stream,
		fallback:  c.fallback,
		fallbackS: c.fallbackS,
	}
}

func (c *Client) resolveProxyForAccount(acc config.Account) (config.Proxy, bool) {
	if c == nil || c.Store == nil {
		return config.Proxy{}, false
	}
	proxyID := strings.TrimSpace(acc.ProxyID)
	if proxyID == "" {
		return config.Proxy{}, false
	}
	snap := c.Store.Snapshot()
	for _, proxyCfg := range snap.Proxies {
		proxyCfg = config.NormalizeProxy(proxyCfg)
		if proxyCfg.ID == proxyID {
			return proxyCfg, true
		}
	}
	return config.Proxy{}, false
}

func (c *Client) requestClientsFromContext(ctx context.Context) requestClients {
	if a, ok := auth.FromContext(ctx); ok {
		return c.requestClientsForAccount(a.Account)
	}
	return c.defaultRequestClients()
}

func (c *Client) requestClientsForAuth(ctx context.Context, a *auth.RequestAuth) requestClients {
	if a != nil {
		return c.requestClientsForAccount(a.Account)
	}
	return c.requestClientsFromContext(ctx)
}

func (c *Client) requestClientsForAccount(acc config.Account) requestClients {
	proxyCfg, ok := c.resolveProxyForAccount(acc)
	if !ok {
		return c.defaultRequestClients()
	}

	key := proxyCacheKey(proxyCfg)
	c.proxyClientsMu.RLock()
	cached, ok := c.proxyClients[key]
	c.proxyClientsMu.RUnlock()
	if ok {
		return cached
	}

	dialContext, err := proxyDialContext(proxyCfg)
	if err != nil {
		config.Logger.Warn("[proxy] build dialer failed", "proxy_id", proxyCfg.ID, "error", err)
		return c.defaultRequestClients()
	}

	bundle := requestClients{
		regular:   trans.NewWithDialContext(60*time.Second, dialContext),
		stream:    trans.NewWithDialContext(0, dialContext),
		fallback:  trans.NewFallbackClient(60*time.Second, dialContext),
		fallbackS: trans.NewFallbackClient(0, dialContext),
	}

	c.proxyClientsMu.Lock()
	if c.proxyClients == nil {
		c.proxyClients = make(map[string]requestClients)
	}
	c.proxyClients[key] = bundle
	c.proxyClientsMu.Unlock()
	return bundle
}

func applyProxyConnectivityHeaders(req *http.Request) {
	if req == nil {
		return
	}
	for key, value := range dsprotocol.BaseHeaders {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		req.Header.Set(key, value)
	}
}

func proxyConnectivityStatus(statusCode int) (bool, string) {
	switch {
	case statusCode >= 200 && statusCode < 300:
		return true, fmt.Sprintf("代理可达，目标返回 HTTP %d", statusCode)
	case statusCode >= 300 && statusCode < 500:
		return true, fmt.Sprintf("代理可达，但目标返回 HTTP %d（可能是风控或挑战）", statusCode)
	default:
		return false, fmt.Sprintf("目标返回 HTTP %d", statusCode)
	}
}
