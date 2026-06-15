package webrpc

func firstOrZero[T any](a []T) T {
	var zero T
	if len(a) == 0 {
		return zero
	}
	return a[0]
}
