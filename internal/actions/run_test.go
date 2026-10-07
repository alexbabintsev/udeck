package actions

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
)

func TestRunDispatchesByType(t *testing.T) {
	if err := Run(nil); err != nil {
		t.Fatal(err)
	}
	if err := Run(&config.Action{Type: "teleport"}); err == nil || !strings.Contains(err.Error(), "unknown action type") {
		t.Fatalf("got %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	if err := Run(&config.Action{Type: "http", URL: srv.URL}); err != nil {
		t.Fatal(err)
	}

	marker := filepath.Join(t.TempDir(), "ran")
	if err := Run(&config.Action{Type: "shell", Cmd: "touch '" + marker + "'"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := exec.Command("test", "-f", marker).Run(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("shell action did not run")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestExplainHTTPErrorWithoutHint(t *testing.T) {
	err := runHTTP("http://\x00invalid")
	if err == nil || strings.Contains(err.Error(), "Is the target") {
		t.Fatalf("got %v", err)
	}
}
