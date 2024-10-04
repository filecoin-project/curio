package paths

import (
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"golang.org/x/xerrors"
)

const StashDirName = "stash"

func (st *Local) StashCreate(ctx context.Context, maxSize int64, writeFunc func(f *os.File) error) (uuid.UUID, error) {
	st.localLk.RLock()

	var selectedPath *path
	var maxAvailable int64

	for _, p := range st.paths {
		if !p.canSeal {
			continue
		}

		stat, _, err := p.stat(st.localStorage)
		if err != nil {
			// Skip path if error getting stat
			continue
		}

		if stat.Available < maxSize {
			// Not enough space
			continue
		}

		if stat.Available > maxAvailable {
			maxAvailable = stat.Available
			selectedPath = p
		}
	}

	st.localLk.RUnlock()

	if selectedPath == nil {
		return uuid.Nil, xerrors.Errorf("no sealing paths have enough space (%d bytes)", maxSize)
	}

	stashDir := filepath.Join(selectedPath.local, StashDirName)
	err := os.MkdirAll(stashDir, 0755)
	if err != nil {
		return uuid.Nil, xerrors.Errorf("creating stash directory %s: %w", stashDir, err)
	}

	fileUUID := uuid.New()
	stashFileName := fileUUID.String() + ".tmp"
	stashFilePath := filepath.Join(stashDir, stashFileName)

	f, err := os.OpenFile(stashFilePath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
	if err != nil {
		return uuid.Nil, xerrors.Errorf("creating stash file %s: %w", stashFilePath, err)
	}

	writeErr := writeFunc(f)
	closeErr := f.Close()

	if writeErr != nil {
		// Remove the stash file since writing failed
		if removeErr := os.Remove(stashFilePath); removeErr != nil {
			log.Errorf("failed to remove stash file %s after write error: %v", stashFilePath, removeErr)
		}
		return uuid.Nil, xerrors.Errorf("writing to stash file %s: %w", stashFilePath, writeErr)
	}

	if closeErr != nil {
		return uuid.Nil, xerrors.Errorf("closing stash file %s: %w", stashFilePath, closeErr)
	}

	return fileUUID, nil
}

func (st *Local) StashURL(id uuid.UUID) (url.URL, error) {
	u, err := url.Parse(st.url)
	if err != nil {
		return url.URL{}, xerrors.Errorf("parsing url %s: %w", st.url, err)
	}

	u.Path = filepath.Join(u.Path, "stash", id.String())
	return *u, nil
}

func (st *Local) StashRemove(ctx context.Context, id uuid.UUID) error {
	st.localLk.RLock()

	fileName := id.String() + ".tmp"

	for _, p := range st.paths {
		if !p.canSeal {
			continue
		}

		st.localLk.RUnlock()

		stashDir := filepath.Join(p.local, StashDirName)
		stashFilePath := filepath.Join(stashDir, fileName)
		if _, err := os.Stat(stashFilePath); err == nil {
			if err := os.Remove(stashFilePath); err != nil {
				return xerrors.Errorf("removing stash file %s: %w", stashFilePath, err)
			}
			return nil
		} else if !os.IsNotExist(err) {
			return xerrors.Errorf("stat stash file %s: %w", stashFilePath, err)
		}
	}

	st.localLk.RUnlock()

	return xerrors.Errorf("stash file %s not found", fileName)
}

func (st *Local) ServeAndRemove(ctx context.Context, id uuid.UUID) (io.ReadCloser, error) {
	st.localLk.RLock()

	fileName := id.String() + ".tmp"

	for _, p := range st.paths {
		if !p.canSeal {
			continue
		}

		stashDir := filepath.Join(p.local, StashDirName)
		stashFilePath := filepath.Join(stashDir, fileName)
		f, err := os.Open(stashFilePath)
		if err == nil {
			st.localLk.RUnlock()

			// Wrap the file in a custom ReadCloser
			return &stashFileReadCloser{
				File:       f,
				path:       stashFilePath,
				eofReached: false,
			}, nil
		} else if !os.IsNotExist(err) {
			return nil, xerrors.Errorf("opening stash file %s: %w", stashFilePath, err)
		}
	}

	st.localLk.RUnlock()

	return nil, xerrors.Errorf("stash file %s not found", fileName)
}

type stashFileReadCloser struct {
	File       *os.File
	path       string
	eofReached bool
}

func (sfrc *stashFileReadCloser) Read(p []byte) (int, error) {
	n, err := sfrc.File.Read(p)
	if err == io.EOF {
		sfrc.eofReached = true
	}
	return n, err
}

func (sfrc *stashFileReadCloser) Close() error {
	err := sfrc.File.Close()
	if err != nil {
		return err
	}
	if sfrc.eofReached {
		// Remove the file
		if err := os.Remove(sfrc.path); err != nil {
			return xerrors.Errorf("removing stash file %s: %w", sfrc.path, err)
		}
	}
	// Else, do not remove the file
	return nil
}

var _ StashStore = &Local{}
