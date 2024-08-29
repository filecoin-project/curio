package storiface

import (
	"errors"
	"fmt"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/proof"
	"github.com/ipfs/go-cid"
)

type WindowPoStResult struct {
	PoStProofs proof.PoStProof
	Skipped    []abi.SectorID
}

type PostSectorChallenge struct {
	SealProof    abi.RegisteredSealProof
	SectorNumber abi.SectorNumber
	SealedCID    cid.Cid
	Challenge    []uint64
	Update       bool
}

type FallbackChallenges struct {
	Sectors    []abi.SectorNumber
	Challenges map[abi.SectorNumber][]uint64
}

type ErrorCode int

const (
	ErrUnknown ErrorCode = iota
)

const (
	// Temp Errors
	ErrTempUnknown ErrorCode = iota + 100
	ErrTempWorkerRestart
	ErrTempAllocateSpace
)

type WorkError interface {
	ErrCode() ErrorCode
}

type CallError struct {
	Code    ErrorCode
	Message string
	sub     error
}

func (c *CallError) ErrCode() ErrorCode {
	return c.Code
}

func (c *CallError) Error() string {
	return fmt.Sprintf("storage call error %d: %s", c.Code, c.Message)
}

func (c *CallError) Unwrap() error {
	if c.sub != nil {
		return c.sub
	}

	return errors.New(c.Message)
}

var _ WorkError = &CallError{}

func Err(code ErrorCode, sub error) *CallError {
	return &CallError{
		Code:    code,
		Message: sub.Error(),

		sub: sub,
	}
}

type WorkerJob struct{} // dummy
