//go:build !darwin

package macos

func FocusedWindow() (Rect, int, error) { return Rect{}, 0, ErrUnsupported }
func SetFocusedWindow(Rect) error       { return ErrUnsupported }
func Screens() ([]Screen, error)        { return nil, ErrUnsupported }
