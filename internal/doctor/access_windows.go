package doctor

// writableDir cannot answer on Windows: access(2)'s emulation reads only the
// read-only attribute, never the ACL that actually denies a write. checked
// is false, and the caller decides whether a probe is allowed where it is.
func writableDir(string) (checked bool, err error) { return false, nil }
