package actions

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strings"
	"testing"
)

func TestRunShellReturnsStartError(t *testing.T) {
	oldExecCommand := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("/definitely-missing-binary-for-test")
	}
	t.Cleanup(func() {
		execCommand = oldExecCommand
	})

	err := runShell("echo hello")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "start shell action") {
		t.Fatalf("expected wrapped start error, got %v", err)
	}
}

func TestRunHTTPReturnsNilOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := runHTTP(srv.URL); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestRunHTTPReturnsErrorOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()

	err := runHTTP(srv.URL)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "http action status") {
		t.Fatalf("expected status error, got %v", err)
	}
}

func TestExplainHTTPErrorIncludesURLAndHint(t *testing.T) {
	err := explainHTTPError("http://127.0.0.1:27655/action/test", &url.Error{
		Op:  "Get",
		URL: "http://127.0.0.1:27655/action/test",
		Err: errors.New("connect: connection refused"),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "127.0.0.1:27655") || !strings.Contains(err.Error(), "running") {
		t.Fatalf("expected helpful http hint, got %v", err)
	}
}

func TestExplainHTTPErrorHidesQueryAndCredentials(t *testing.T) {
	raw := "https://user:pw@hooks.example.com/hook?token=s3cret"
	err := explainHTTPError(raw, &url.Error{Op: "Get", URL: raw, Err: errors.New("tls: bad certificate")})
	msg := err.Error()
	if strings.Contains(msg, "s3cret") || strings.Contains(msg, "pw") {
		t.Fatalf("error leaks secrets: %v", msg)
	}
	if !strings.Contains(msg, "https://hooks.example.com/hook") {
		t.Fatalf("error should name the endpoint: %v", msg)
	}
	if got := logSafeURL("::bad"); got != "(invalid url)" {
		t.Fatalf("invalid url: %q", got)
	}
}
