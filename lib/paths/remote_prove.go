package paths

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/filecoin-project/curio/lib/tarutil"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"github.com/hashicorp/go-multierror"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// ReadMinCacheInto reads finalized-like (few MiB) cache files into the target dir
func (r *Remote) ReadMinCacheInto(ctx context.Context, s storiface.SectorRef, ft storiface.SectorFileType, w io.Writer) error {
	paths, _, err := r.local.AcquireSector(ctx, s, ft, storiface.FTNone, storiface.PathStorage, storiface.AcquireMove)
	if err != nil {
		return xerrors.Errorf("acquire local: %w", err)
	}

	path := storiface.PathByType(paths, ft)
	if path != "" {
		return readMinCache(path, w)
	}

	si, err := r.index.StorageFindSector(ctx, s.ID, ft, 0, false)
	if err != nil {
		log.Debugf("Reader, did not find file on any of the workers %s (%s)", path, ft.String())
		return err
	}

	if len(si) == 0 {
		return xerrors.Errorf("failed to read sector %v from remote(%d): %w", s, ft, storiface.ErrSectorNotFound)
	}

	sort.Slice(si, func(i, j int) bool {
		return si[i].Weight > si[j].Weight
	})

	for _, info := range si {
		for _, u := range info.URLs {
			purl, err := url.Parse(u)
			if err != nil {
				log.Warnw("parsing url", "url", u, "error", err)
				continue
			}

			purl.RawQuery = "mincache=true"

			u = purl.String()

			rd, err := r.readRemote(ctx, u, 0, 0)
			if err != nil {
				log.Warnw("reading from remote", "url", u, "error", err)
				continue
			}

			_, err = io.Copy(w, rd)

			if cerr := rd.Close(); cerr != nil {
				log.Errorf("failed to close reader: %v", err)
				continue
			}

			if err != nil {
				log.Warnw("copying from remote", "url", u, "error", err)
				continue
			}

			return nil
		}
	}

	return xerrors.Errorf("failed to read minimal sector cache %v from remote(%d): %w", s, ft, storiface.ErrSectorNotFound)
}

func readMinCache(dir string, writer io.Writer) error {
	return tarutil.TarDirectory(tarutil.FinCacheFileConstraints, dir, writer, make([]byte, 1<<20))
}

func (r *Remote) GenerateSingleVanillaProof(ctx context.Context, minerID abi.ActorID, sinfo storiface.PostSectorChallenge, ppt abi.RegisteredPoStProof) ([]byte, error) {
	p, err := r.local.GenerateSingleVanillaProof(ctx, minerID, sinfo, ppt)
	if err != errPathNotFound {
		return p, err
	}

	sid := abi.SectorID{
		Miner:  minerID,
		Number: sinfo.SectorNumber,
	}

	ft := storiface.FTSealed | storiface.FTCache
	if sinfo.Update {
		ft = storiface.FTUpdate | storiface.FTUpdateCache
	}

	si, err := r.index.StorageFindSector(ctx, sid, ft, 0, false)
	if err != nil {
		return nil, xerrors.Errorf("finding sector %d failed: %w", sid, err)
	}

	requestParams := SingleVanillaParams{
		Miner:     minerID,
		Sector:    sinfo,
		ProofType: ppt,
	}
	jreq, err := json.Marshal(requestParams)
	if err != nil {
		return nil, err
	}

	merr := xerrors.Errorf("sector not found")

	for _, info := range si {
		for _, u := range info.BaseURLs {
			url := fmt.Sprintf("%s/vanilla/single", u)

			req, err := http.NewRequest("POST", url, strings.NewReader(string(jreq)))
			if err != nil {
				merr = multierror.Append(merr, xerrors.Errorf("request: %w", err))
				log.Warnw("GenerateSingleVanillaProof request failed", "url", url, "error", err)
				continue
			}

			if r.auth != nil {
				req.Header = r.auth.Clone()
			}
			req = req.WithContext(ctx)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				merr = multierror.Append(merr, xerrors.Errorf("do request: %w", err))
				log.Warnw("GenerateSingleVanillaProof do request failed", "url", url, "error", err)
				continue
			}

			if resp.StatusCode != http.StatusOK {
				if resp.StatusCode == http.StatusNotFound {
					log.Debugw("reading vanilla proof from remote not-found response", "url", url, "store", info.ID)
					continue
				}
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					merr = multierror.Append(merr, xerrors.Errorf("resp.Body ReadAll: %w", err))
					log.Warnw("GenerateSingleVanillaProof read response body failed", "url", url, "error", err)
					continue
				}

				if err := resp.Body.Close(); err != nil {
					log.Error("response close: ", err)
				}

				merr = multierror.Append(merr, xerrors.Errorf("non-200 code from %s: '%s'", url, strings.TrimSpace(string(body))))
				log.Warnw("GenerateSingleVanillaProof non-200 code from remote", "code", resp.StatusCode, "url", url, "body", string(body))
				continue
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				if err := resp.Body.Close(); err != nil {
					log.Error("response close: ", err)
				}

				merr = multierror.Append(merr, xerrors.Errorf("resp.Body ReadAll: %w", err))
				log.Warnw("GenerateSingleVanillaProof read response body failed", "url", url, "error", err)
				continue
			}

			_ = resp.Body.Close()

			return body, nil
		}
	}

	return nil, merr
}

func (r *Remote) GeneratePoRepVanillaProof(ctx context.Context, sr storiface.SectorRef, sealed, unsealed cid.Cid, ticket abi.SealRandomness, seed abi.InteractiveSealRandomness) ([]byte, error) {
	// Attempt to generate the proof locally first
	p, err := r.local.GeneratePoRepVanillaProof(ctx, sr, sealed, unsealed, ticket, seed)
	if err != errPathNotFound {
		return p, err
	}

	// Define the file types to look for based on the sector's state
	ft := storiface.FTSealed | storiface.FTCache

	// Find sector information
	si, err := r.index.StorageFindSector(ctx, sr.ID, ft, 0, false)
	if err != nil {
		return nil, xerrors.Errorf("finding sector %d failed: %w", sr.ID, err)
	}

	// Prepare request parameters
	requestParams := PoRepVanillaParams{
		Sector:   sr,
		Sealed:   sealed,
		Unsealed: unsealed,
		Ticket:   ticket,
		Seed:     seed,
	}
	jreq, err := json.Marshal(requestParams)
	if err != nil {
		return nil, err
	}

	merr := xerrors.Errorf("sector not found")

	// Iterate over all found sector locations
	for _, info := range si {
		for _, u := range info.BaseURLs {
			url := fmt.Sprintf("%s/vanilla/porep", u)

			// Create and send the request
			req, err := http.NewRequest("POST", url, strings.NewReader(string(jreq)))
			if err != nil {
				merr = multierror.Append(merr, xerrors.Errorf("request: %w", err))
				log.Warnw("GeneratePoRepVanillaProof request failed", "url", url, "error", err)
				continue
			}

			// Set auth headers if available
			if r.auth != nil {
				req.Header = r.auth.Clone()
			}
			req = req.WithContext(ctx)

			// Execute the request
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				merr = multierror.Append(merr, xerrors.Errorf("do request: %w", err))
				log.Warnw("GeneratePoRepVanillaProof do request failed", "url", url, "error", err)
				continue
			}

			// Handle non-OK status codes
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()

				if resp.StatusCode == http.StatusNotFound {
					log.Debugw("reading vanilla proof from remote not-found response", "url", url, "store", info.ID)
					continue
				}

				merr = multierror.Append(merr, xerrors.Errorf("non-200 code from %s: '%s'", url, strings.TrimSpace(string(body))))
				log.Warnw("GeneratePoRepVanillaProof non-200 code from remote", "code", resp.StatusCode, "url", url, "body", string(body))
				continue
			}

			// Read the response body
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				merr = multierror.Append(merr, xerrors.Errorf("resp.Body ReadAll: %w", err)) //nolint:ineffassign
				log.Warnw("GeneratePoRepVanillaProof read response body failed", "url", url, "error", err)
			}
			_ = resp.Body.Close()

			// Return the proof if successful
			return body, nil
		}
	}

	// Return the accumulated error if the proof was not generated
	return nil, merr
}

func (r *Remote) ReadSnapVanillaProof(ctx context.Context, sr storiface.SectorRef) ([]byte, error) {
	// Attempt to generate the proof locally first
	p, err := r.local.ReadSnapVanillaProof(ctx, sr)
	if err != errPathNotFound {
		return p, err
	}

	// Define the file types to look for based on the sector's state
	ft := storiface.FTUpdateCache

	// Find sector information
	si, err := r.index.StorageFindSector(ctx, sr.ID, ft, 0, false)
	if err != nil {
		return nil, xerrors.Errorf("finding sector %d failed: %w", sr.ID, err)
	}

	// Prepare request parameters
	requestParams := SnapVanillaParams{
		Sector: sr,
	}
	jreq, err := json.Marshal(requestParams)
	if err != nil {
		return nil, err
	}

	merr := xerrors.Errorf("sector not found")

	// Iterate over all found sector locations
	for _, info := range si {
		for _, u := range info.BaseURLs {
			url := fmt.Sprintf("%s/vanilla/snap", u)

			// Create and send the request
			req, err := http.NewRequest("POST", url, strings.NewReader(string(jreq)))
			if err != nil {
				merr = multierror.Append(merr, xerrors.Errorf("request: %w", err))
				log.Warnw("ReadSnapVanillaProof request failed", "url", url, "error", err)
				continue
			}

			// Set auth headers if available
			if r.auth != nil {
				req.Header = r.auth.Clone()
			}
			req = req.WithContext(ctx)

			// Execute the request
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				merr = multierror.Append(merr, xerrors.Errorf("do request: %w", err))
				log.Warnw("ReadSnapVanillaProof do request failed", "url", url, "error", err)
				continue
			}

			// Handle non-OK status codes
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()

				if resp.StatusCode == http.StatusNotFound {
					log.Debugw("reading snap vanilla proof from remote not-found response", "url", url, "store", info.ID)
					continue
				}

				merr = multierror.Append(merr, xerrors.Errorf("non-200 code from %s: '%s'", url, strings.TrimSpace(string(body))))
				log.Warnw("ReadSnapVanillaProof non-200 code from remote", "code", resp.StatusCode, "url", url, "body", string(body))
				continue
			}

			// Read the response body
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				merr = multierror.Append(merr, xerrors.Errorf("resp.Body ReadAll: %w", err)) //nolint:ineffassign
				log.Warnw("ReadSnapVanillaProof read response body failed", "url", url, "error", err)
			}
			_ = resp.Body.Close()

			// Return the proof if successful
			return body, nil
		}
	}

	// Return the accumulated error if the proof was not generated
	return nil, merr
}
