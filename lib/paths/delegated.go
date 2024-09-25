package paths

/*
 * Premium Feature Module - Proprietary License
 *
 * Copyright (C) 2024 Curio Storage Inc.
 *
 * This file is part of Curio but is **not** licensed under the MIT or Apache 2.0 licenses.
 * It contains proprietary code intended for use only by authorized users.
 *
 * **Source Visibility**:
 * The source code is provided for viewing and inspection purposes only.
 * Users are permitted to read and review the code but are prohibited from modifying it.
 *
 * **Redistribution Permissions**:
 * Redistribution of this code in source and binary forms, without modification, is permitted
 * provided that the following conditions are met:
 *
 * 1. **Source Code Redistribution**:
 *    - Must retain this header and all associated legal notices.
 * 2. **Binary Form Redistribution**:
 *    - Must reproduce this header and all associated legal notices in the documentation and/or other materials provided with the distribution.
 * 3. **No Modification**:
 *    - The premium feature code must remain unmodified. Any modification is strictly prohibited.
 *
 * **Unauthorized Use Prohibited**:
 * Unauthorized copying, modification, or use of this code, via any medium, is strictly prohibited.
 * Unauthorized use may result in legal action.
 *
 * **License Agreement**:
 * Authorized users are granted a non-transferable, non-sublicensable, and non-exclusive license
 * to use this code in accordance with the terms set forth in the
 * [Premium Features License Agreement](link-to-license-agreement).
 *
 * **Disclaimer of Warranty**:
 * THIS SOFTWARE IS PROVIDED "AS IS," WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED,
 * INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE, AND NON-INFRINGEMENT.
 * IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM,
 * DAMAGES, OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT, OR OTHERWISE,
 * ARISING FROM, OUT OF, OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
 */

import (
	"context"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/storage/sealer/fsutil"
	"github.com/ipfs/go-cid"
	"github.com/samber/lo"
	"golang.org/x/xerrors"
	"io"
)

var ObjectStorageUUIDMagic = storiface.ID("aaaaa76e-e870-4123-bce0-6203f9000001")

type SectorHandle interface {
	io.ReadSeeker
}

type ObjectStorageProvider interface {
	PutSectorData(fileType storiface.SectorFileType, sid abi.SectorID, reader io.Reader) error
	GetSectorData(fileType storiface.SectorFileType, sid abi.SectorID) (SectorHandle, error)
	RemoveSectorData(fileType storiface.SectorFileType, sid abi.SectorID) error
	GenerateSingleVanillaProof(ctx context.Context, minerID abi.ActorID, si storiface.PostSectorChallenge, ppt abi.RegisteredPoStProof) ([]byte, error)
}

// Delegated implements object-storage backed sector storage.
// It can only be used for long-term storage
type Delegated struct {
	provider ObjectStorageProvider
	parent   Store
}

func (b *Delegated) AcquireSector(ctx context.Context, sref storiface.SectorRef, existing storiface.SectorFileType, allocate storiface.SectorFileType, sealing storiface.PathType, op storiface.AcquireMode, opts ...storiface.AcquireOption) (paths storiface.SectorPaths, stores storiface.SectorPaths, err error) {
	if existing|allocate != existing^allocate {
		return storiface.SectorPaths{}, storiface.SectorPaths{}, xerrors.New("can't both find and allocate a sector")
	}

	p, s, err := b.parent.AcquireSector(ctx, sref, existing, allocate, sealing, op, opts...)
	if err != nil {
		return storiface.SectorPaths{}, storiface.SectorPaths{}, err
	}

	// TODO: check that the sector wasn't in object storage, if it was error that Acquire doesn't support this

	return p, s, nil
}

func (b *Delegated) Remove(ctx context.Context, s abi.SectorID, types storiface.SectorFileType, force bool, keepIn []storiface.ID) error {
	if !lo.Contains(keepIn, ObjectStorageUUIDMagic) {
		if err := b.provider.RemoveSectorData(types, s); err != nil {
			return xerrors.Errorf("removing sector from object storage: %w", err)
		}
	}

	return b.parent.Remove(ctx, s, types, force, keepIn)
}

func (b *Delegated) RemoveCopies(ctx context.Context, s abi.SectorID, types storiface.SectorFileType) error {
	// RemoveCopies is only used during sealing, not supported for object storage

	return b.parent.RemoveCopies(ctx, s, types)
}

func (b *Delegated) MoveStorage(ctx context.Context, s storiface.SectorRef, types storiface.SectorFileType, opts ...storiface.AcquireOption) error {
	//TODO implement me
	panic("implement me")

	// if targetting LTS, get a reader from parent and put in object storage
}

func (b *Delegated) FsStat(ctx context.Context, id storiface.ID) (fsutil.FsStat, error) {
	if id == ObjectStorageUUIDMagic {
		// todo return real values
		return fsutil.FsStat{
			Capacity:    100 << 30,
			Available:   100 << 30,
			FSAvailable: 100 << 30,
			Reserved:    0,
			Max:         0,
			Used:        0,
		}, nil
	}

	return b.parent.FsStat(ctx, id)
}

func (b *Delegated) Reserve(ctx context.Context, sid storiface.SectorRef, ft storiface.SectorFileType, storageIDs storiface.SectorPaths, overheadTab map[storiface.SectorFileType]int, minFreePercentage float64) (func(), error) {
	//TODO implement me
	panic("implement me")
}

func (b *Delegated) GenerateSingleVanillaProof(ctx context.Context, minerID abi.ActorID, si storiface.PostSectorChallenge, ppt abi.RegisteredPoStProof) ([]byte, error) {
	//TODO implement me
	panic("implement me")
}

func (b *Delegated) GeneratePoRepVanillaProof(ctx context.Context, sr storiface.SectorRef, sealed, unsealed cid.Cid, ticket abi.SealRandomness, seed abi.InteractiveSealRandomness) ([]byte, error) {
	// sealing only
	return b.parent.GeneratePoRepVanillaProof(ctx, sr, sealed, unsealed, ticket, seed)
}

func (b *Delegated) ReadSnapVanillaProof(ctx context.Context, sr storiface.SectorRef) ([]byte, error) {
	// sealing only
	return b.parent.ReadSnapVanillaProof(ctx, sr)
}

// TODO: Read* methods, add those to the Store interface

var _ Store = &Delegated{}
