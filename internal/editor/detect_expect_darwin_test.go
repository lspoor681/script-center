//go:build darwin

package editor

// platformDetect returns the macOS detection routine, which exists on this
// platform because detect_darwin_impl.go is compiled here.
func platformDetect() []Editor { return DetectDarwin() }
