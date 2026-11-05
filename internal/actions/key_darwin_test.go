//go:build darwin

package actions

import (
	"github.com/alexbabintsev/udeck/internal/config"
	"reflect"
	"strings"
	"testing"
)

func TestParseShortcutParsesModifiersAndKey(t *testing.T) {
	spec, err := parseShortcut("command shift t")
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if spec.keyCode != keyCodes["t"] {
		t.Fatalf("unexpected key code: got %d want %d", spec.keyCode, keyCodes["t"])
	}
	gotCodes := make([]int, 0, len(spec.modifiers))
	for _, mod := range spec.modifiers {
		gotCodes = append(gotCodes, mod.code)
	}
	wantCodes := []int{modifierKeys["cmd"].code, modifierKeys["shift"].code}
	if !reflect.DeepEqual(gotCodes, wantCodes) {
		t.Fatalf("unexpected modifiers: got %v want %v", gotCodes, wantCodes)
	}
}

func TestParseShortcutRejectsMultipleKeys(t *testing.T) {
	_, err := parseShortcut("cmd t r")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "multiple key names") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSendKeyRequiresAccessibility(t *testing.T) {
	oldTrusted := isAXTrusted
	oldPrompt := promptAXAccess
	isAXTrusted = func() bool { return false }
	prompted := false
	promptAXAccess = func() { prompted = true }
	t.Cleanup(func() {
		isAXTrusted = oldTrusted
		promptAXAccess = oldPrompt
	})

	err := sendKey("cmd t")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Accessibility") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !prompted {
		t.Fatal("expected accessibility prompt request")
	}
}

func TestSendKeyUsesParsedShortcut(t *testing.T) {
	oldTrusted := isAXTrusted
	oldSend := sendShortcutFunc
	isAXTrusted = func() bool { return true }
	var got parsedShortcut
	sendShortcutFunc = func(spec parsedShortcut) error {
		got = spec
		return nil
	}
	t.Cleanup(func() {
		isAXTrusted = oldTrusted
		sendShortcutFunc = oldSend
	})

	if err := sendKey("ctrl option enter"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if got.keyCode != keyCodes["return"] {
		t.Fatalf("unexpected key code: got %d want %d", got.keyCode, keyCodes["return"])
	}
	gotCodes := make([]int, 0, len(got.modifiers))
	for _, mod := range got.modifiers {
		gotCodes = append(gotCodes, mod.code)
	}
	wantCodes := []int{modifierKeys["ctrl"].code, modifierKeys["alt"].code}
	if !reflect.DeepEqual(gotCodes, wantCodes) {
		t.Fatalf("unexpected modifiers: got %v want %v", gotCodes, wantCodes)
	}
}

func TestRequestKeyPermissionsPromptsOnlyWhenNeeded(t *testing.T) {
	oldTrusted := isAXTrusted
	oldPrompt := promptAXAccess
	promptCount := 0
	promptAXAccess = func() { promptCount++ }
	t.Cleanup(func() {
		isAXTrusted = oldTrusted
		promptAXAccess = oldPrompt
	})

	isAXTrusted = func() bool { return false }
	RequestKeyPermissions()
	if promptCount != 1 {
		t.Fatalf("unexpected prompt count after untrusted request: got %d want 1", promptCount)
	}

	isAXTrusted = func() bool { return true }
	RequestKeyPermissions()
	if promptCount != 1 {
		t.Fatalf("unexpected prompt count after trusted request: got %d want 1", promptCount)
	}
}

func TestNormalizeKeyTokenAliases(t *testing.T) {
	for in, want := range map[string]string{"command": "cmd", "control": "ctrl", "option": "alt", "enter": "return", "x": "x"} {
		if got := normalizeKeyToken(in); got != want {
			t.Fatalf("normalizeKeyToken(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRunKeyActionUsesSender(t *testing.T) {
	oldTrusted, oldSend := isAXTrusted, sendShortcutFunc
	defer func() { isAXTrusted, sendShortcutFunc = oldTrusted, oldSend }()
	isAXTrusted = func() bool { return true }
	sent := false
	sendShortcutFunc = func(parsedShortcut) error { sent = true; return nil }
	if err := Run(&config.Action{Type: "key", Key: "cmd c"}); err != nil || !sent {
		t.Fatalf("err=%v sent=%v", err, sent)
	}
}
