package testutils

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"math/bits"
	"os"
	"strings"
	"time"

	"github.com/ipfs/boxo/blockservice"
	bstore "github.com/ipfs/boxo/blockstore"
	chunk "github.com/ipfs/boxo/chunker"
	"github.com/ipfs/boxo/exchange/offline"
	"github.com/ipfs/boxo/files"
	"github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/boxo/ipld/unixfs/importer/balanced"
	ihelper "github.com/ipfs/boxo/ipld/unixfs/importer/helpers"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-cidutil"
	ds "github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	ipldformat "github.com/ipfs/go-ipld-format"
	carv2 "github.com/ipld/go-car/v2"
	"github.com/ipld/go-car/v2/blockstore"
	"github.com/multiformats/go-multihash"
	"github.com/oklog/ulid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-data-segment/datasegment"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/commcidv2"
)

const defaultHashFunction = uint64(multihash.BLAKE2B_MIN + 31)

func CreateRandomTmpFile(dir string, size int64) (string, error) {
	source := io.LimitReader(rand.Reader, size)

	file, err := os.CreateTemp(dir, "sourcefile.dat")
	if err != nil {
		return "", err
	}

	buff := make([]byte, 4<<20)

	n, err := io.CopyBuffer(file, source, buff)
	if err != nil {
		return "", err
	}

	if n != size {
		return "", fmt.Errorf("incorrect file size: written %d != expected %d", n, size)
	}

	//
	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return "", err
	}

	return file.Name(), nil
}

func CreateDenseCARWith(dir, src string, chunksize int64, maxlinks int, caropts []carv2.Option) (cid.Cid, string, error) {
	bs := bstore.NewBlockstore(dssync.MutexWrap(ds.NewMapDatastore()))
	dagSvc := merkledag.NewDAGService(blockservice.New(bs, offline.Exchange(bs)))

	root, err := WriteUnixfsDAGTo(src, dagSvc, chunksize, maxlinks)
	if err != nil {
		return cid.Undef, "", err
	}

	// Create a UnixFS DAG again AND generate a CARv2 file using a CARv2
	// read-write blockstore now that we have the root.
	out, err := os.CreateTemp(dir, "rand")
	if err != nil {
		return cid.Undef, "", err
	}
	err = out.Close()
	if err != nil {
		return cid.Undef, "", err
	}

	rw, err := blockstore.OpenReadWrite(out.Name(), []cid.Cid{root}, caropts...)
	if err != nil {
		return cid.Undef, "", err
	}

	dagSvc = merkledag.NewDAGService(blockservice.New(rw, offline.Exchange(rw)))

	root2, err := WriteUnixfsDAGTo(src, dagSvc, chunksize, maxlinks)
	if err != nil {
		return cid.Undef, "", err
	}

	err = rw.Finalize()
	if err != nil {
		return cid.Undef, "", err
	}

	if root != root2 {
		return cid.Undef, "", fmt.Errorf("DAG root cid mismatch")
	}

	return root, out.Name(), nil
}

func WriteUnixfsDAGTo(path string, into ipldformat.DAGService, chunksize int64, maxlinks int) (cid.Cid, error) {
	file, err := os.Open(path)
	if err != nil {
		return cid.Undef, err
	}
	defer func() {
		_ = file.Close()
	}()

	stat, err := file.Stat()
	if err != nil {
		return cid.Undef, err
	}

	// get a IPLD reader path file
	// required to write the Unixfs DAG blocks to a filestore
	rpf, err := files.NewReaderPathFile(file.Name(), file, stat)
	if err != nil {
		return cid.Undef, err
	}

	// generate the dag and get the root
	// import to UnixFS
	prefix, err := merkledag.PrefixForCidVersion(1)
	if err != nil {
		return cid.Undef, err
	}

	prefix.MhType = defaultHashFunction

	bufferedDS := ipldformat.NewBufferedDAG(context.Background(), into)
	params := ihelper.DagBuilderParams{
		Maxlinks:  maxlinks,
		RawLeaves: true,
		// NOTE: InlineBuilder not recommended, we are using this to test identity CIDs
		CidBuilder: cidutil.InlineBuilder{
			Builder: prefix,
			Limit:   126,
		},
		Dagserv: bufferedDS,
		NoCopy:  true,
	}

	db, err := params.New(chunk.NewSizeSplitter(rpf, chunksize))
	if err != nil {
		return cid.Undef, err
	}

	nd, err := balanced.Layout(db)
	if err != nil {
		return cid.Undef, err
	}

	err = bufferedDS.Commit()
	if err != nil {
		return cid.Undef, err
	}

	err = rpf.Close()
	if err != nil {
		return cid.Undef, err
	}

	return nd.Cid(), nil
}

func CreateAggregateFromCars(files []string, dealSize abi.PaddedPieceSize, aggregateOut bool) (cid.Cid, error) {
	var lines []string
	var readers []io.Reader
	var deals []abi.PieceInfo

	for _, f := range files {
		file, err := os.Open(f)
		if err != nil {
			return cid.Undef, xerrors.Errorf("opening subpiece file: %w", err)
		}
		stat, err := file.Stat()
		if err != nil {
			return cid.Undef, xerrors.Errorf("getting file stat: %w", err)
		}
		cp := new(commp.Calc)
		_, err = io.Copy(cp, file)
		if err != nil {
			return cid.Undef, xerrors.Errorf("copying subpiece to commp writer: %w", err)
		}
		_, err = file.Seek(0, io.SeekStart)
		if err != nil {
			return cid.Undef, xerrors.Errorf("seeking to start of file: %w", err)
		}
		pbytes, size, err := cp.Digest()
		if err != nil {
			return cid.Undef, xerrors.Errorf("computing digest for subpiece: %w", err)
		}
		comm, err := commcidv2.NewSha2CommP(uint64(stat.Size()), pbytes)
		if err != nil {
			return cid.Undef, xerrors.Errorf("converting data commitment to CID: %w", err)
		}
		deals = append(deals, abi.PieceInfo{
			PieceCID: comm.PCidV1(),
			Size:     abi.PaddedPieceSize(size),
		})
		readers = append(readers, file)
		urlStr := fmt.Sprintf("http://piece-server:12320/pieces?id=%s", stat.Name())
		lines = append(lines, fmt.Sprintf("%s\t%s", comm.PCidV2().String(), urlStr))
	}

	_, upsize, err := datasegment.ComputeDealPlacement(deals)
	if err != nil {
		return cid.Undef, xerrors.Errorf("computing deal placement: %w", err)
	}

	next := 1 << (64 - bits.LeadingZeros64(upsize+256))

	if abi.PaddedPieceSize(next) != dealSize {
		return cid.Undef, fmt.Errorf("deal size mismatch: expected %d, got %d", dealSize, abi.PaddedPieceSize(next))
	}

	a, err := datasegment.NewAggregate(abi.PaddedPieceSize(next), deals)
	if err != nil {
		return cid.Undef, xerrors.Errorf("creating aggregate: %w", err)
	}
	out, err := a.AggregateObjectReader(readers)
	if err != nil {
		return cid.Undef, xerrors.Errorf("creating aggregate reader: %w", err)
	}

	x, err := ulid.New(uint64(time.Now().UnixMilli()), rand.Reader)
	if err != nil {
		return cid.Undef, xerrors.Errorf("creating aggregate file: %w", err)
	}

	f, err := os.OpenFile(x.String(), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
	if err != nil {
		return cid.Undef, err
	}
	defer func() {
		_ = f.Close()
	}()

	cp := new(commp.Calc)
	w := io.MultiWriter(cp, f)

	n, err := io.Copy(w, out)
	if err != nil {
		_ = f.Close()
		return cid.Undef, xerrors.Errorf("writing aggregate: %w", err)
	}

	_ = f.Close()

	digest, paddedPieceSize, err := cp.Digest()
	if err != nil {
		return cid.Undef, xerrors.Errorf("computing digest: %w", err)
	}
	if abi.PaddedPieceSize(paddedPieceSize) != dealSize {
		return cid.Undef, fmt.Errorf("deal size mismatch after final commP: expected %d, got %d", dealSize, abi.PaddedPieceSize(paddedPieceSize))
	}

	if n != int64(dealSize.Unpadded()) {
		return cid.Undef, fmt.Errorf("incorrect aggregate raw size: expected %d, got %d", dealSize.Unpadded(), n)
	}

	comm, err := commcidv2.NewSha2CommP(uint64(n), digest)
	if err != nil {
		return cid.Undef, xerrors.Errorf("creating commP: %w", err)
	}

	err = os.WriteFile(fmt.Sprintf("aggregate_%s", comm.PCidV2().String()), []byte(strings.Join(lines, "\n")), 0644)
	if err != nil {
		return cid.Undef, xerrors.Errorf("writing aggregate to file: %w", err)
	}

	if !aggregateOut {
		defer func() {
			_ = os.Remove(f.Name())
		}()
	} else {
		defer func() {
			_ = os.Rename(f.Name(), fmt.Sprintf("aggregate_%s.piece", comm.PCidV2().String()))
		}()
	}

	return comm.PCidV2(), nil
}

func EnvElse(env, els string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return els
}
