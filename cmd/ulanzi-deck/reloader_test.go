package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fastReloader(t *testing.T) {
	t.Helper()
	old := configReloadPollInterval
	configReloadPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { configReloadPollInterval = old })
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func expectChange(t *testing.T, r *configReloader, want bool) {
	t.Helper()
	select {
	case <-r.Changes():
		if !want {
			t.Fatal("unexpected change event")
		}
	case <-time.After(200 * time.Millisecond):
		if want {
			t.Fatal("expected change event")
		}
	}
}

// Content sizes differ on every rewrite so detection does not depend on
// filesystem mtime granularity.
func TestConfigReloaderDetectsIconChanges(t *testing.T) {
	fastReloader(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	iconPath := filepath.Join(dir, "icon.png")
	writeFile(t, cfgPath, "buttons: []\n")
	writeFile(t, iconPath, "icon-a")

	r := newConfigReloader(cfgPath, []string{iconPath})
	defer r.Close()
	expectChange(t, r, false)

	writeFile(t, iconPath, "icon-bb")
	expectChange(t, r, true)

	if err := os.Remove(iconPath); err != nil {
		t.Fatal(err)
	}
	expectChange(t, r, true)
}

func TestConfigReloaderDetectsConfigChanges(t *testing.T) {
	fastReloader(t)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, cfgPath, "buttons: []\n")
	r := newConfigReloader(cfgPath, nil)
	defer r.Close()
	writeFile(t, cfgPath, "buttons: [ ]\n")
	expectChange(t, r, true)
}

func TestConfigReloaderUpdatePathsSwapsWatchedIcons(t *testing.T) {
	fastReloader(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	iconA := filepath.Join(dir, "a.png")
	iconB := filepath.Join(dir, "b.png")
	writeFile(t, cfgPath, "buttons: []\n")
	writeFile(t, iconA, "a1")
	writeFile(t, iconB, "b1")

	r := newConfigReloader(cfgPath, []string{iconA})
	defer r.Close()
	r.UpdatePaths(cfgPath, []string{iconB, iconB, ""})

	writeFile(t, iconA, "a22")
	expectChange(t, r, false)

	writeFile(t, iconB, "b22")
	expectChange(t, r, true)
}

func TestStatFileMissing(t *testing.T) {
	if st := statFile(filepath.Join(t.TempDir(), "missing")); st.exists {
		t.Fatal("missing file must not exist")
	}
}
