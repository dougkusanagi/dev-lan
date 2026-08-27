//go:build !windows

package platform

import "os"

func replaceFileAtomic(source, target string) error {
	return os.Rename(source, target)
}
