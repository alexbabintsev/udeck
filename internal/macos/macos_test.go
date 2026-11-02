package macos

import "testing"

func TestParseMediaKey(t *testing.T) {
	for name, want := range map[string]MediaKey{"play_pause": MediaPlayPause, "next": MediaNext, "previous": MediaPrevious} {
		if got, err := ParseMediaKey(name); err != nil || got != want {
			t.Fatalf("%s: %v %v", name, got, err)
		}
	}
	if _, err := ParseMediaKey("stop"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestScopeString(t *testing.T) {
	if Output.String() != "output" || Input.String() != "input" {
		t.Fatal("Scope.String")
	}
}
