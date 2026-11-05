//go:build personal

package widgets

import (
	"log"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/alexbabintsev/udeck/internal/config"
)

const (
	siteDefaultPoll = time.Minute
	siteMinPoll     = 30 * time.Second
	siteHTTPTimeout = 8 * time.Second
)

var siteHTTPClient = &http.Client{Timeout: siteHTTPTimeout}

// siteAPI is the JSON metrics endpoint shared by the site_* widgets.
type siteAPI struct {
	baseURL string
	token   string
}

func newSiteAPI(cfg *config.Widget) siteAPI {
	api := siteAPI{
		baseURL: strings.TrimRight(strings.TrimSpace(cfg.URL), "/"),
		token:   strings.TrimSpace(cfg.Token),
	}
	if api.token != "" {
		warnClearCredentials("site", api.baseURL)
	}
	return api
}

// get fetches baseURL+path into out, sending the bearer token if set.
func (a siteAPI) get(path string, out any) error {
	headers := map[string]string{"Accept": "application/json"}
	if a.token != "" {
		headers["Authorization"] = "Bearer " + a.token
	}
	return getJSON(siteHTTPClient, a.baseURL+path, headers, out)
}

// sitePoll converts poll_ms into an interval, never below siteMinPoll.
func sitePoll(ms int) time.Duration {
	if ms <= 0 {
		return siteDefaultPoll
	}
	d := time.Duration(ms) * time.Millisecond
	if d < siteMinPoll {
		return siteMinPoll
	}
	return d
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
