package retrieval

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/go-chi/chi/v5"
	"github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/boxo/ipld/unixfs"
	"github.com/ipfs/go-cid"
	ipldformat "github.com/ipfs/go-ipld-format"
	car "github.com/ipld/go-car"
	"github.com/ipld/go-car/util"
	mh "github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/storiface"
)

type sniffReaderAt struct {
	data      []byte
	lastSize  int
	readCount int
}

func (s *sniffReaderAt) ReadAt(p []byte, off int64) (int, error) {
	s.readCount++
	s.lastSize = len(p)
	if off >= int64(len(s.data)) {
		return 0, io.EOF
	}
	n := copy(p, s.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func TestSniffContentTypeReadsExactWindow(t *testing.T) {
	payload := append([]byte("%PDF-1.4"), bytes.Repeat([]byte("x"), 4096)...)
	r := &sniffReaderAt{data: payload}

	ct, err := sniffContentType(r)
	require.NoError(t, err)
	require.Equal(t, "application/pdf", ct)
	require.Equal(t, 1, r.readCount)
	require.Equal(t, int(MIME_SNIFF_BYTES), r.lastSize)
}

func TestSniffContentTypeShortPiece(t *testing.T) {
	r := &sniffReaderAt{data: []byte("hello")}
	ct, err := sniffContentType(r)
	require.NoError(t, err)
	require.Equal(t, "text/plain; charset=utf-8", ct)
	require.Equal(t, int(MIME_SNIFF_BYTES), r.lastSize)
}

func TestServeContentHeadRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodHead, "/piece/test", nil)
	w := httptest.NewRecorder()

	http.ServeContent(w, req, "", lastModified, bytes.NewReader([]byte("0123456789")))

	res := w.Result()
	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Equal(t, "bytes", res.Header.Get("Accept-Ranges"))
	require.Equal(t, "10", res.Header.Get("Content-Length"))
	require.Equal(t, lastModified.UTC().Format(http.TimeFormat), res.Header.Get("Last-Modified"))
	require.Empty(t, body)
}

func TestServeContentRangeRequestReturnsPartial(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/piece/test", nil)
	req.Header.Set("Range", "bytes=2-4")
	w := httptest.NewRecorder()

	http.ServeContent(w, req, "", lastModified, bytes.NewReader([]byte("0123456789")))

	res := w.Result()
	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusPartialContent, res.StatusCode)
	require.Equal(t, "bytes 2-4/10", res.Header.Get("Content-Range"))
	require.Equal(t, "3", res.Header.Get("Content-Length"))
	require.Equal(t, []byte("234"), body)
}

func TestSetHeadersWritesQuotedETag(t *testing.T) {
	pieceCid, err := cid.Parse("bafkqaaa")
	require.NoError(t, err)

	w := httptest.NewRecorder()
	setHeaders(w, pieceCid, "video/mp4")

	require.Equal(t, `"`+pieceCid.String()+`"`, w.Header().Get("ETag"))
}

type byteReader struct {
	*bytes.Reader
}

func (byteReader) Close() error { return nil }

type aclFiles struct {
	files map[cid.Cid][]byte
}

func (a *aclFiles) HasPiece(_ context.Context, pc cid.Cid) (bool, error) {
	_, ok := a.files[pc]
	return ok, nil
}

func (a *aclFiles) ReadPiece(_ context.Context, pc cid.Cid, _ int64) (storiface.Reader, error) {
	data, ok := a.files[pc]
	if !ok {
		return nil, nil
	}
	return byteReader{bytes.NewReader(data)}, nil
}

func TestHandleDownload(t *testing.T) {
	payload := bytes.Repeat([]byte("hello acl piece"), 10)
	v1, err := cid.Decode("baga6ea4seaqomqafu276g53zko4k23xzh4h4uecjwicbmvhsuqi7o4bhthhm4aq")
	require.NoError(t, err)
	v2, err := commcid.PieceCidV2FromV1(v1, uint64(len(payload)))
	require.NoError(t, err)

	cpr := &cachedreader.CachedPieceReader{}
	cpr.SetACLPieceReader(&aclFiles{files: map[cid.Cid][]byte{v2: payload}})
	mux := chi.NewMux()
	Router(mux, &Provider{cpr: cpr}, testDenyFilter())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/download/" + v2.String())
	require.NoError(t, err)
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	require.Equal(t, http.StatusUnauthorized, res.StatusCode)
	require.Equal(t, "FOC-Grant", res.Header.Get("WWW-Authenticate"))
	require.Empty(t, body)

	head, err := http.Head(srv.URL + "/download/" + v2.String())
	require.NoError(t, err)
	require.NoError(t, head.Body.Close())
	require.Equal(t, http.StatusUnauthorized, head.StatusCode)
	require.Equal(t, "FOC-Grant", head.Header.Get("WWW-Authenticate"))

	miss, err := http.Get(srv.URL + "/download/" + v1.String())
	require.NoError(t, err)
	require.NoError(t, miss.Body.Close())
	require.Equal(t, http.StatusNotFound, miss.StatusCode)

	// A decoded foc_grant passes the missing-grant check. This provider has no blockchain, so the read stops there.
	token := base64.RawURLEncoding.EncodeToString([]byte("query-grant"))
	granted, err := http.Get(srv.URL + "/download/" + v2.String() + "?foc_grant=" + token)
	require.NoError(t, err)
	require.NoError(t, granted.Body.Close())
	require.Equal(t, http.StatusServiceUnavailable, granted.StatusCode)
}

func TestHandleDownloadCAREnvelopeGrantStatuses(t *testing.T) {
	v1, err := cid.Decode("baga6ea4seaqomqafu276g53zko4k23xzh4h4uecjwicbmvhsuqi7o4bhthhm4aq")
	require.NoError(t, err)

	wrapped := makeEncryptedFeeCAR(t, testFEEBytes(t))
	wrappedV2, err := commcid.PieceCidV2FromV1(v1, uint64(len(wrapped)))
	require.NoError(t, err)

	multi := makeTwoFileCAR(t)
	multiV2, err := commcid.PieceCidV2FromV1(v1, uint64(len(multi)))
	require.NoError(t, err)

	cpr := &cachedreader.CachedPieceReader{}
	cpr.SetACLPieceReader(&aclFiles{files: map[cid.Cid][]byte{
		wrappedV2: wrapped,
		multiV2:   multi,
	}})
	provider := &Provider{cpr: cpr}
	provider.SetBlockChain(big.NewInt(314159))
	mux := chi.NewMux()
	Router(mux, provider, testDenyFilter())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	token := base64.RawURLEncoding.EncodeToString([]byte("not-a-real-grant"))
	badGrant, err := http.Get(srv.URL + "/download/" + wrappedV2.String() + "?foc_grant=" + token)
	require.NoError(t, err)
	require.NoError(t, badGrant.Body.Close())
	require.Equal(t, http.StatusForbidden, badGrant.StatusCode)

	multiRes, err := http.Get(srv.URL + "/download/" + multiV2.String() + "?foc_grant=" + token)
	require.NoError(t, err)
	require.NoError(t, multiRes.Body.Close())
	require.Equal(t, http.StatusUnauthorized, multiRes.StatusCode)
}

func testFEEBytes(t *testing.T) []byte {
	t.Helper()
	protected, err := cbor.Marshal(map[int64]any{
		1:  int64(-65793),
		16: "application/vnd.filecoin-encryption+cose",
		5:  []byte{1, 2, 3, 4, 5, 6, 7},
		-1: uint64(262144),
		-65792: map[string]any{
			"foc/v":    uint64(1),
			"foc/own":  "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266",
			"foc/ks":   "0x00112233445566778899aabbccddeeff",
			"foc/salt": "0x000102030405060708090a0b0c0d0e0f",
		},
	})
	require.NoError(t, err)
	body, err := cbor.Marshal([]any{protected, map[int64]any{}, nil})
	require.NoError(t, err)
	tagged, err := cbor.Marshal(cbor.RawTag{Number: 16, Content: body})
	require.NoError(t, err)
	return append(tagged, []byte("ciphertext")...)
}

func makeEncryptedFeeCAR(t *testing.T, payload []byte) []byte {
	t.Helper()
	fileCID, fileData, leafCID, leafData := makeUnixFSFile(t, payload)
	dirCID, dirData := makeUnixFSDir(t, "encrypted.fee", fileCID, uint64(len(fileData)+len(leafData)))
	return writeCAR(t, dirCID, []struct {
		c    cid.Cid
		data []byte
	}{
		{dirCID, dirData},
		{fileCID, fileData},
		{leafCID, leafData},
	})
}

func makeTwoFileCAR(t *testing.T) []byte {
	t.Helper()
	fileA, fileAData, leafA, leafAData := makeUnixFSFile(t, testFEEBytes(t))
	fileB, fileBData, leafB, leafBData := makeUnixFSFile(t, []byte("other"))
	dir := new(merkledag.ProtoNode)
	require.NoError(t, dir.SetCidBuilder(merkledag.V1CidPrefix()))
	fsn := unixfs.NewFSNode(unixfs.TDirectory)
	fsBytes, err := fsn.GetBytes()
	require.NoError(t, err)
	dir.SetData(fsBytes)
	require.NoError(t, dir.AddRawLink("encrypted.fee", &ipldformat.Link{Name: "encrypted.fee", Size: uint64(len(fileAData) + len(leafAData)), Cid: fileA}))
	require.NoError(t, dir.AddRawLink("other.bin", &ipldformat.Link{Name: "other.bin", Size: uint64(len(fileBData) + len(leafBData)), Cid: fileB}))
	return writeCAR(t, dir.Cid(), []struct {
		c    cid.Cid
		data []byte
	}{
		{dir.Cid(), dir.RawData()},
		{fileA, fileAData},
		{fileB, fileBData},
		{leafA, leafAData},
		{leafB, leafBData},
	})
}

func makeUnixFSFile(t *testing.T, payload []byte) (fileCID cid.Cid, fileData []byte, leafCID cid.Cid, leafData []byte) {
	t.Helper()
	h, err := mh.Sum(payload, mh.SHA2_256, -1)
	require.NoError(t, err)
	leafCID = cid.NewCidV1(cid.Raw, h)
	leafData = payload

	fsn := unixfs.NewFSNode(unixfs.TFile)
	fsn.AddBlockSize(uint64(len(payload)))
	fsBytes, err := fsn.GetBytes()
	require.NoError(t, err)
	nd := new(merkledag.ProtoNode)
	require.NoError(t, nd.SetCidBuilder(merkledag.V1CidPrefix()))
	nd.SetData(fsBytes)
	require.NoError(t, nd.AddRawLink("", &ipldformat.Link{Name: "", Size: uint64(len(payload)), Cid: leafCID}))
	return nd.Cid(), nd.RawData(), leafCID, leafData
}

func makeUnixFSDir(t *testing.T, name string, fileCID cid.Cid, fileSize uint64) (cid.Cid, []byte) {
	t.Helper()
	fsn := unixfs.NewFSNode(unixfs.TDirectory)
	fsBytes, err := fsn.GetBytes()
	require.NoError(t, err)
	nd := new(merkledag.ProtoNode)
	require.NoError(t, nd.SetCidBuilder(merkledag.V1CidPrefix()))
	nd.SetData(fsBytes)
	require.NoError(t, nd.AddRawLink(name, &ipldformat.Link{Name: name, Size: fileSize, Cid: fileCID}))
	return nd.Cid(), nd.RawData()
}

func writeCAR(t *testing.T, root cid.Cid, blocks []struct {
	c    cid.Cid
	data []byte
}) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, car.WriteHeader(&car.CarHeader{Roots: []cid.Cid{root}, Version: 1}, &buf))
	for _, b := range blocks {
		require.NoError(t, util.LdWrite(&buf, b.c.Bytes(), b.data))
	}
	return buf.Bytes()
}
