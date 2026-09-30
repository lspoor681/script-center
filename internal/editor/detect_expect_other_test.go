//go:build !windows && !darwin

package editor

// platformDetect returns the unix detection routine, which exists on this
// platform because detect_other.go is compiled here.
func platformDetect() []Editor { return DetectUnix() }
