//go:build !unix

package pager

// watchResize is a no-op off Unix, where there is no window-change signal to
// listen for. The pager still works; it simply does not reflow until a key is
// pressed.
func watchResize() (<-chan struct{}, func()) {
	return nil, func() {}
}
