package actions

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
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

func TestRunBindingExecutesChainInOrder(t *testing.T) {
	var got []string
	oldSleep := sleepFunc
	oldHTTPClient := httpClient
	sleepFunc = func(d time.Duration) {
		got = append(got, "sleep:"+d.String())
	}
	httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		got = append(got, req.URL.String())
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       http.NoBody,
			Header:     make(http.Header),
		}, nil
	})}
	t.Cleanup(func() {
		sleepFunc = oldSleep
		httpClient = oldHTTPClient
	})

	err := RunBinding(&config.GestureBinding{
		Chain: []config.ChainStep{
			{Action: &config.Action{Type: "http", URL: "http://example.test/one"}},
			{DelayMS: 250},
			{Action: &config.Action{Type: "http", URL: "http://example.test/two"}},
		},
	})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	want := []string{
		"http://example.test/one",
		"sleep:250ms",
		"http://example.test/two",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected chain order: got %v want %v", got, want)
	}
}

func TestRunBindingRunsDirectAction(t *testing.T) {
	var called bool
	oldHTTPClient := httpClient
	httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       http.NoBody,
			Header:     make(http.Header),
		}, nil
	})}
	t.Cleanup(func() {
		httpClient = oldHTTPClient
	})

	err := RunBinding(&config.GestureBinding{Type: "http", URL: "http://example.test/direct"})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if !called {
		t.Fatal("expected direct action to run")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
