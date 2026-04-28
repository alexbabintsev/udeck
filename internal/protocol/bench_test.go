package protocol

import (
	"path/filepath"
	"testing"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/icons"
)

func BenchmarkIconLoad(b *testing.B) {
	path := filepath.Join("..", "..", "examples", "test-icons", "notes.png")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		data, err := icons.Load(path, icons.Insets{})
		if err != nil {
			b.Fatal(err)
		}
		if len(data) == 0 {
			b.Fatal("empty icon data")
		}
	}
}

func BenchmarkPageZIPSingleButton(b *testing.B) {
	buttons := []config.Button{
		{
			At:    [2]int{0, 0},
			Label: "Memory",
			Icon:  filepath.Join("..", "..", "examples", "test-icons", "notes.png"),
		},
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		data, err := PageZIP(buttons)
		if err != nil {
			b.Fatal(err)
		}
		if len(data) == 0 {
			b.Fatal("empty zip data")
		}
	}
}

func BenchmarkPageZIPFullPage(b *testing.B) {
	base := filepath.Join("..", "..", "examples", "test-icons")
	buttons := []config.Button{
		{At: [2]int{0, 0}, Label: "Apps", Icon: filepath.Join(base, "apps-folder.png")},
		{At: [2]int{1, 0}, Label: "Work", Icon: filepath.Join(base, "work-folder.png")},
		{At: [2]int{2, 0}, Label: "System", Icon: filepath.Join(base, "system-folder.png")},
		{At: [2]int{3, 0}, Label: "Media", Icon: filepath.Join(base, "media-folder.png")},
		{At: [2]int{4, 0}, Label: "Reload", Icon: filepath.Join(base, "reload.png")},
		{At: [2]int{0, 1}, Label: "Find", Icon: filepath.Join(base, "find.png")},
		{At: [2]int{1, 1}, Label: "Play", Icon: filepath.Join(base, "space.png")},
		{At: [2]int{2, 1}, Label: "New Tab", Icon: filepath.Join(base, "tab.png")},
		{At: [2]int{3, 1}, Label: "Notes", Icon: filepath.Join(base, "notes.png")},
		{At: [2]int{4, 1}, Label: "Memory", Icon: filepath.Join(base, "notes.png")},
		{At: [2]int{0, 2}, Label: "Escape", Icon: filepath.Join(base, "escape.png")},
		{At: [2]int{1, 2}, Label: "Mute", Icon: filepath.Join(base, "mute.png")},
		{At: [2]int{2, 2}, Label: "Lock", Icon: filepath.Join(base, "lock.png")},
		{At: [2]int{3, 2}, Label: "Shot", Icon: filepath.Join(base, "screenshot.png")},
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		data, err := PageZIP(buttons)
		if err != nil {
			b.Fatal(err)
		}
		if len(data) == 0 {
			b.Fatal("empty zip data")
		}
	}
}
