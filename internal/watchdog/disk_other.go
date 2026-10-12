//go:build !unix

package watchdog

import "errors"

// DiskFreeFraction на не-unix системах недоступна: правило пропускается.
func DiskFreeFraction(string) (float64, error) { return 0, errors.New("disk check unsupported") }
