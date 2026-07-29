//go:build !unix

package persistence

// defaultProcessAlive cannot probe pid liveness on non-unix platforms, so it
// reports every pid as alive; staleness then falls back to the age guards.
func defaultProcessAlive(_ int) bool {
	return true
}
