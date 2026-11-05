package widgets

import (
	"net/http"
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
