//go:build !windows

package platform

// ScheduleDeferredRemoval is only needed on Windows, where the running
// executable is locked. Other platforms can unlink an executable in use and
// therefore need no helper.
func ScheduleDeferredRemoval(...string) error { return nil }
