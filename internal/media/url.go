package media

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultURLLimits provides reasonable defaults for URL ingestion limits.
var DefaultURLLimits = URLLimits{
	AllowedSchemes:    []string{"http", "https"},
	MaxDownloadBytes:  50 * 1024 * 1024, // 50 MB
	DownloadTimeout:   30 * time.Second,
	MaxRedirects:      5,
	AllowPrivateIP:    false,
	AllowLoopback:     false,
	AllowLinkLocal:    false,
	AllowUniqueLocal:  false,
}

// URLLimits configures security and resource constraints for URL ingestion.
type URLLimits struct {
	// AllowedSchemes restricts which URL schemes are accepted.
	// Default: ["http", "https"]
	AllowedSchemes []string

	// MaxDownloadBytes limits the response body size.
	// Default: 50 MB
	MaxDownloadBytes int64

	// DownloadTimeout is the maximum time allowed for a single download.
	// Default: 30s
	DownloadTimeout time.Duration

	// MaxRedirects limits the number of HTTP redirects followed.
	// Default: 5
	MaxRedirects int

	// AllowPrivateIP permits downloads from RFC 1918 / RFC 4193 addresses.
	// Default: false
	AllowPrivateIP bool

	// AllowLoopback permits downloads from 127.0.0.0/8 and ::1.
	// Default: false
	AllowLoopback bool

	// AllowLinkLocal permits downloads from 169.254.0.0/16 and fe80::/10.
	// Default: false
	AllowLinkLocal bool

	// AllowUniqueLocal permits downloads from fc00::/7 (ULA).
	// Default: false
	AllowUniqueLocal bool
}

// ValidateURL checks if a URL passes scheme and SSRF policy checks.
// It does not perform network I/O.
func (l URLLimits) ValidateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Check scheme
	scheme := u.Scheme
	if scheme == "" {
		scheme = "http"
	}
	allowed := false
	for _, s := range l.AllowedSchemes {
		if s == scheme {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("scheme %q not allowed (allowed: %v)", scheme, l.AllowedSchemes)
	}

	// Check host is present
	if u.Host == "" {
		return fmt.Errorf("URL has no host")
	}

	// SSRF protection: resolve the host and check the IP
	host := u.Hostname()
	ips, err := net.LookupIP(host)
	if err != nil {
		// If we can't resolve, reject — could be a DNS rebinding attack
		return fmt.Errorf("cannot resolve host %q: %w", host, err)
	}

	for _, ip := range ips {
		if err := l.checkIP(ip); err != nil {
			return err
		}
	}

	return nil
}

// checkIP validates a single IP address against SSRF policy.
func (l URLLimits) checkIP(ip net.IP) error {
	ip = ip.To4() // normalize to IPv4 for simpler checks
	if ip == nil {
		// IPv6
		return l.checkIPv6(net.IP(ip))
	}

	// Loopback (127.0.0.0/8)
	if ip[0] == 127 {
		if !l.AllowLoopback {
			return fmt.Errorf("loopback address %s not allowed", ip)
		}
	}

	// Link-local (169.254.0.0/16)
	if ip[0] == 169 && ip[1] == 254 {
		if !l.AllowLinkLocal {
			return fmt.Errorf("link-local address %s not allowed", ip)
		}
	}

	// Private ranges (RFC 1918)
	// 10.0.0.0/8
	if ip[0] == 10 {
		if !l.AllowPrivateIP {
			return fmt.Errorf("private address %s not allowed", ip)
		}
	}
	// 172.16.0.0/12
	if ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31 {
		if !l.AllowPrivateIP {
			return fmt.Errorf("private address %s not allowed", ip)
		}
	}
	// 192.168.0.0/16
	if ip[0] == 192 && ip[1] == 168 {
		if !l.AllowPrivateIP {
			return fmt.Errorf("private address %s not allowed", ip)
		}
	}

	// 0.0.0.0
	if ip[0] == 0 && ip[1] == 0 && ip[2] == 0 && ip[3] == 0 {
		return fmt.Errorf("unspecified address %s not allowed", ip)
	}

	return nil
}

// checkIPv6 validates an IPv6 address against SSRF policy.
func (l URLLimits) checkIPv6(ip net.IP) error {
	// Loopback (::1)
	if ip.To4() != nil {
		return nil // handled by IPv4 check
	}

	// Check for ::1
	if len(ip) == 16 && ip[15] == 1 && ip[0] == 0 && ip[1] == 0 &&
		ip[2] == 0 && ip[3] == 0 && ip[4] == 0 && ip[5] == 0 &&
		ip[6] == 0 && ip[7] == 0 && ip[8] == 0 && ip[9] == 0 &&
		ip[10] == 0 && ip[11] == 0 && ip[12] == 0 && ip[13] == 0 &&
		ip[14] == 0 {
		if !l.AllowLoopback {
			return fmt.Errorf("loopback address %s not allowed", ip)
		}
	}

	// Unique local (fc00::/7)
	if len(ip) >= 2 && ((ip[0]&0xfe) == 0xfc) {
		if !l.AllowUniqueLocal {
			return fmt.Errorf("unique local address %s not allowed", ip)
		}
	}

	// Link-local (fe80::/10)
	if len(ip) >= 2 && ((ip[0]&0xfe) == 0xfe) && ((ip[1]&0xc0) == 0x80) {
		if !l.AllowLinkLocal {
			return fmt.Errorf("link-local address %s not allowed", ip)
		}
	}

	return nil
}

// DownloadURL performs an HTTP GET with security constraints and returns
// the response body reader and content type. The caller must close the
// returned reader. Returns an error if the URL fails validation or the
// download exceeds limits.
func (l URLLimits) DownloadURL(ctx context.Context, rawURL string) (io.ReadCloser, string, error) {
	if err := l.ValidateURL(rawURL); err != nil {
		return nil, "", fmt.Errorf("URL validation failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set a User-Agent to identify the client
	req.Header.Set("User-Agent", "Squid-OS/1.0")

	client := &http.Client{
		Timeout: l.DownloadTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= l.MaxRedirects {
				return fmt.Errorf("too many redirects (limit: %d)", l.MaxRedirects)
			}
			// Validate the redirect target for SSRF
			if err := l.ValidateURL(req.URL.String()); err != nil {
				return fmt.Errorf("redirect target blocked: %w", err)
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("download failed: %w", err)
	}

	// Check status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, "", fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Limit response body size
	body := &limitedReadCloser{
		ReadCloser: resp.Body,
		limit:      l.MaxDownloadBytes,
	}

	return body, resp.Header.Get("Content-Type"), nil
}

// limitedReadCloser wraps an io.ReadCloser and enforces a byte limit.
type limitedReadCloser struct {
	io.ReadCloser
	limit    int64
	consumed int64
}

func (l *limitedReadCloser) Read(p []byte) (n int, err error) {
	remaining := l.limit - l.consumed
	if remaining <= 0 {
		return 0, fmt.Errorf("download size limit exceeded (%d bytes)", l.limit)
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err = l.ReadCloser.Read(p)
	l.consumed += int64(n)
	if l.consumed > l.limit {
		return n, fmt.Errorf("download size limit exceeded (%d bytes)", l.limit)
	}
	if l.consumed == l.limit {
		// If there was an error but we also hit the limit exactly,
		// prefer the limit error.
		if err != nil {
			return n, fmt.Errorf("download size limit exceeded (%d bytes)", l.limit)
		}
		// Add EOF to signal no more data should be read past the limit
		if n > 0 {
			return n, nil
		}
	}
	return n, err
}

// IsSafeURL performs a lightweight check that a URL is allowed for ingestion
// without performing DNS resolution. This is useful for pre-validation.
func IsSafeURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// GuessFilenameFromURL extracts a filename from a URL path.
// Returns the last path segment or "download" if no path is available.
func GuessFilenameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "download"
	}
	path := u.Path
	if path == "" || path == "/" {
		if u.Host != "" {
			return u.Host
		}
		return "download"
	}
	// Strip trailing slash
	path = strings.TrimRight(path, "/")
	// Return last segment
	segments := strings.Split(path, "/")
	return segments[len(segments)-1]
}
