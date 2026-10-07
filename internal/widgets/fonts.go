package widgets

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/opentype"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/debug"
)

// Widget text uses a custom TTF/OTF when widgets.font is configured and
// the embedded Go Mono family otherwise, so text renders on every
// machine without system fonts.
var (
	fontMu     sync.Mutex
	customFont *opentype.Font
	customPath string

	builtinOnce    sync.Once
	builtinRegular *opentype.Font
	builtinBold    *opentype.Font
)

// SetFont selects the font file used for widget text; "" restores the
// built-in font. A file that cannot be loaded is logged and ignored.
func SetFont(path string) {
	path = strings.TrimSpace(path)
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}

	fontMu.Lock()
	defer fontMu.Unlock()
	if path == customPath {
		return
	}
	customPath, customFont = path, nil
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		debug.Logf("widget font %q: %v; using built-in font", path, err)
		return
	}
	ft, err := opentype.Parse(data)
	if err != nil {
		debug.Logf("widget font %q: %v; using built-in font", path, err)
		return
	}
	customFont = ft
}

// uiFont returns the font for widget text. A custom font is used for both
// weights; the built-in family has a real bold.
func uiFont(bold bool) *opentype.Font {
	fontMu.Lock()
	ft := customFont
	fontMu.Unlock()
	if ft != nil {
		return ft
	}
	builtinOnce.Do(func() {
		builtinRegular, _ = opentype.Parse(gomono.TTF)
		builtinBold, _ = opentype.Parse(gomonobold.TTF)
	})
	if bold {
		return builtinBold
	}
	return builtinRegular
}
