package api

import "fmt"

// ChainError wraps errors from chain API calls so their origin can be
// distinguished from other kinds of errors (e.g. database, storage).
// Use errors.As to check for chain errors:
//
//	var chainErr *api.ChainError
//	if errors.As(err, &chainErr) {
//	    // handle chain-specific error
//	}
type ChainError struct {
	Err error
}

func (e *ChainError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("chain: %v", e.Err)
	}
	return "chain: unknown error"
}

func (e *ChainError) Unwrap() error {
	return e.Err
}
