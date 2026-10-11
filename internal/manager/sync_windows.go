package manager

// syncDir on Windows is a no-op: directory fsync is unsupported there, and
// os.Rename on the same volume is atomic enough for this use case.
func syncDir(string) error { return nil }
