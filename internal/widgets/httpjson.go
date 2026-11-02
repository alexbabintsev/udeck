package widgets

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

// maxHTTPBodyBytes caps every widget HTTP response body, so a misbehaving
// or hostile endpoint cannot make the daemon buffer unbounded data.
const maxHTTPBodyBytes = 4 << 20

// maxArtworkPixels bounds remote images before they are decoded.
const maxArtworkPixels = 4096 * 4096

// httpGet fetches rawURL and returns the body, failing on non-2xx status
// or a body larger than maxHTTPBodyBytes.
func httpGet(client *http.Client, rawURL string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		// url.Error prints the full URL, query string included; logs
		// only get the host and path.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return nil, fmt.Errorf("GET %s%s: %w", req.URL.Host, req.URL.Path, urlErr.Err)
		}
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s: status %s", req.URL.Path, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxHTTPBodyBytes {
		return nil, fmt.Errorf("GET %s: response larger than %d bytes", req.URL.Path, maxHTTPBodyBytes)
	}
	return body, nil
}

// getJSON fetches rawURL and decodes the JSON body into out.
func getJSON(client *http.Client, rawURL string, headers map[string]string, out any) error {
	body, err := httpGet(client, rawURL, headers)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode %T: %w", out, err)
	}
	return nil
}

// sendsCredentialsInClear reports whether credentials sent to rawURL
// cross the network unencrypted: plain http to a host that is neither
// loopback nor a private (LAN/VPN) address.
func sendsCredentialsInClear(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || !strings.EqualFold(u.Scheme, "http") {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return false
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return !addr.IsLoopback() && !addr.IsPrivate()
	}
	return true // a host name: assume it is reached over the internet
}

// warnClearCredentials logs once per widget creation when its credentials
// would travel over plain HTTP.
func warnClearCredentials(widget, rawURL string) {
	if sendsCredentialsInClear(rawURL) {
		u, _ := url.Parse(rawURL)
		log.Printf("warning: %s widget sends credentials over plain http to %s; use https", widget, u.Host)
	}
}
