package platform

// AtomicReplaceFile publishes a file through the platform-specific atomic
// replacement primitive. Windows needs MoveFileEx with replace/write-through;
// Unix can use rename on the same filesystem.
func AtomicReplaceFile(source, target string) error {
	return replaceFileAtomic(source, target)
}
