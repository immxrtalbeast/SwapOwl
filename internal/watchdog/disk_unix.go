//go:build unix

package watchdog

import "syscall"

// DiskFreeFraction — доля свободного места на файловой системе path.
func DiskFreeFraction(path string) (float64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	if st.Blocks == 0 {
		return 1, nil
	}
	return float64(st.Bavail) / float64(st.Blocks), nil
}
