//go:build windows

package editor

// platformDetect returns the Windows detection routine, which exists on this
// platform because detect_windows.go is compiled here.
func platformDetect() []Editor { return DetectWindows() }
