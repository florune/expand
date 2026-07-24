//go:build !windows

package hotkey

import "errors"

type implementation struct{}

func (implementation) start(func(target uintptr)) error {
	return errors.New("global hotkey is currently available on Windows only")
}
func (implementation) stop() {}
func (implementation) insertText(uintptr, string, int) (string, error) {
	return "", errors.New("inserting into another application is currently available on Windows only")
}
func (implementation) copySelection(uintptr) error {
	return errors.New("reading a selection is currently available on Windows only")
}
func (implementation) foregroundWindow() uintptr { return 0 }
func (implementation) isExternalTarget(uintptr) bool {
	return false
}
