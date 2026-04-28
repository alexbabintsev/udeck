package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigReloaderDetectsIconChanges(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	iconPath := filepath.Join(dir, "icon.png")

	if err := os.WriteFile(cfgPath, []byte("buttons: []\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(iconPath, []byte("icon-a"), 0o644); err != nil {
		t.Fatalf("write icon: %v", err)
	}

	r := newConfigReloader(cfgPath, []string{iconPath})
	defer r.Close()

	time.Sleep(1100 * time.Millisecond)

	if err := os.WriteFile(iconPath, []byte("icon-b"), 0o644); err != nil {
		t.Fatalf("rewrite icon: %v", err)
	}

	select {
	case <-r.Changes():
	case <-time.After(3 * time.Second):
		t.Fatal("expected reloader to detect icon change")
	}
}

func TestConfigReloaderUpdatePathsSwapsWatchedIcons(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	iconA := filepath.Join(dir, "a.png")
	iconB := filepath.Join(dir, "b.png")

	if err := os.WriteFile(cfgPath, []byte("buttons: []\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(iconA, []byte("a1"), 0o644); err != nil {
		t.Fatalf("write iconA: %v", err)
	}
	if err := os.WriteFile(iconB, []byte("b1"), 0o644); err != nil {
		t.Fatalf("write iconB: %v", err)
	}

	r := newConfigReloader(cfgPath, []string{iconA})
	defer r.Close()

	r.UpdatePaths(cfgPath, []string{iconB})
	time.Sleep(1100 * time.Millisecond)

	if err := os.WriteFile(iconA, []byte("a2"), 0o644); err != nil {
		t.Fatalf("rewrite iconA: %v", err)
	}

	select {
	case <-r.Changes():
		t.Fatal("unexpected change event for removed icon")
	case <-time.After(1500 * time.Millisecond):
	}

	if err := os.WriteFile(iconB, []byte("b2"), 0o644); err != nil {
		t.Fatalf("rewrite iconB: %v", err)
	}

	select {
	case <-r.Changes():
	case <-time.After(3 * time.Second):
		t.Fatal("expected reloader to detect new icon change")
	}
}
