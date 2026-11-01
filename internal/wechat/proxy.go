package wechat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// A local install usually has a dynamic public IP, which it cannot keep in the
// account's IP allowlist. A SOCKS5 proxy gives the push a fixed egress address;
// only this package's traffic uses it, Ark calls stay direct.

type socksProxy struct {
	addr       string
	user, pass string
}

var (
	errProxyURL       = errors.New("proxy must look like socks5://user:password@host:port")
	errProxyDial      = errors.New("SOCKS5 proxy connection failed")
	errProxyHandshake = errors.New("SOCKS5 proxy handshake failed")
)

// parseProxy validates the stored proxy URL. An empty string means "direct".
func parseProxy(raw string) (*socksProxy, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Port() == "" {
		return nil, errProxyURL
	}
	switch strings.ToLower(u.Scheme) {
	case "socks5", "socks5h":
	default:
		return nil, errProxyURL
	}
	proxy := &socksProxy{addr: u.Host}
	if u.User != nil {
		proxy.user = u.User.Username()
		proxy.pass, _ = u.User.Password()
		if proxy.user == "" || len(proxy.user) > 255 || len(proxy.pass) > 255 {
			return nil, errProxyURL
		}
	}
	return proxy, nil
}

// ValidateProxy reports whether a proxy URL can be used, without dialing it.
func ValidateProxy(raw string) error {
	_, err := parseProxy(raw)
	return err
}

func (p *socksProxy) client() *http.Client {
	transport := &http.Transport{DialContext: p.dial, TLSHandshakeTimeout: 15 * time.Second}
	return &http.Client{
		Timeout:       60 * time.Second,
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// dial opens a SOCKS5 (RFC 1928) tunnel to addr, with the username/password
// method of RFC 1929 when credentials are present. Hostnames are passed to the
// proxy unresolved, so the account sees the proxy's egress IP either way.
func (p *socksProxy) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", p.addr)
	if err != nil {
		return nil, errProxyDial
	}
	done := false
	defer func() {
		if !done {
			conn.Close()
		}
	}()
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}

	methods := []byte{0x00}
	if p.user != "" {
		methods = []byte{0x00, 0x02}
	}
	if _, err := conn.Write(append([]byte{0x05, byte(len(methods))}, methods...)); err != nil {
		return nil, errProxyHandshake
	}
	var choice [2]byte
	if _, err := io.ReadFull(conn, choice[:]); err != nil || choice[0] != 0x05 {
		return nil, errProxyHandshake
	}
	switch choice[1] {
	case 0x00:
	case 0x02:
		if p.user == "" {
			return nil, errProxyHandshake
		}
		auth := []byte{0x01, byte(len(p.user))}
		auth = append(auth, p.user...)
		auth = append(auth, byte(len(p.pass)))
		auth = append(auth, p.pass...)
		if _, err := conn.Write(auth); err != nil {
			return nil, errProxyHandshake
		}
		var status [2]byte
		if _, err := io.ReadFull(conn, status[:]); err != nil || status[0] != 0x01 || status[1] != 0x00 {
			return nil, errProxyHandshake
		}
	default:
		return nil, errProxyHandshake
	}

	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, errProxyHandshake
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return nil, errProxyHandshake
	}
	request := []byte{0x05, 0x01, 0x00}
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			request = append(append(request, 0x01), v4...)
		} else {
			request = append(append(request, 0x04), ip.To16()...)
		}
	} else {
		if len(host) > 255 {
			return nil, errProxyHandshake
		}
		request = append(request, 0x03, byte(len(host)))
		request = append(request, host...)
	}
	request = append(request, byte(port>>8), byte(port))
	if _, err := conn.Write(request); err != nil {
		return nil, errProxyHandshake
	}

	var head [4]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil || head[0] != 0x05 {
		return nil, errProxyHandshake
	}
	if head[1] != 0x00 {
		return nil, fmt.Errorf("SOCKS5 proxy refused the connection (code %d)", head[1])
	}
	skip := 0
	switch head[3] {
	case 0x01:
		skip = 4
	case 0x04:
		skip = 16
	case 0x03:
		var length [1]byte
		if _, err := io.ReadFull(conn, length[:]); err != nil {
			return nil, errProxyHandshake
		}
		skip = int(length[0])
	default:
		return nil, errProxyHandshake
	}
	if _, err := io.ReadFull(conn, make([]byte, skip+2)); err != nil {
		return nil, errProxyHandshake
	}
	conn.SetDeadline(time.Time{})
	done = true
	return conn, nil
}
