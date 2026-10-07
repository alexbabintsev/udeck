package widgets

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
