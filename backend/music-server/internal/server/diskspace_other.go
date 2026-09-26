//go:build !linux

package server

func diskSpace(string) (free, total uint64, ok bool) {
	return 0, 0, false
}
