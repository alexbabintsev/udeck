//go:build !personal

package widgets

import (
	"strings"
	"testing"

	"github.com/alexbabintsev/udeck/internal/config"
)

func TestPersonalWidgetsAreNotBuilt(t *testing.T) {
	for _, typ := range []string{"freqtrade", "site", "project"} {
		buttons := []config.Button{{RuntimeID: "x", Widget: &config.Widget{Type: typ}}}
		m, err := NewManager(&config.Config{Buttons: buttons})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.ResolveButtons(buttons); err == nil || !strings.Contains(err.Error(), "unsupported") {
			t.Fatalf("%s: want unsupported, got %v", typ, err)
		}
		_ = m.Close()
	}
}
