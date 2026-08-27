//go:build !windows

package platform

func RemoveUserPathEntry(string) error { return nil }
