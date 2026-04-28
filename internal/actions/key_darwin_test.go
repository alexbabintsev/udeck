//go:build darwin

package actions

import (
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
	isAXTrusted = func() bool { return false }
	t.Cleanup(func() {
		isAXTrusted = oldTrusted
	})

	err := sendKey("cmd t")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Accessibility") {
		t.Fatalf("unexpected error: %v", err)
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
