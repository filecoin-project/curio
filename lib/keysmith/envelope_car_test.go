package keysmith

import (
	"bytes"
	"testing"

	"github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/boxo/ipld/unixfs"
	"github.com/ipfs/go-cid"
	ipldformat "github.com/ipfs/go-ipld-format"
	car "github.com/ipld/go-car"
	"github.com/ipld/go-car/util"
	mh "github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"
)

func TestReadEnvelopeLabelsFromCARFileRoot(t *testing.T) {
	meta := validMeta()
	env := envelopeBytes(t, meta)
	carBytes := makeFileRootCAR(t, env)

	labels, err := ReadEnvelopeLabels(bytes.NewReader(carBytes))
	require.NoError(t, err)
	require.Equal(t, meta["foc/own"], labels.Owner)
	require.Equal(t, meta["foc/ks"], labels.Keyspace)
	require.Equal(t, meta["foc/salt"], labels.Salt)

	fromAt, err := ReadEnvelopeLabelsAt(bytes.NewReader(carBytes))
	require.NoError(t, err)
	require.Equal(t, labels, fromAt)
}

func TestReadEnvelopeLabelsFromCAREncryptedFee(t *testing.T) {
	meta := validMeta()
	env := envelopeBytes(t, meta)
	carBytes := makeNamedFileCAR(t, encryptedFeeName, env)

	labels, err := ReadEnvelopeLabels(bytes.NewReader(carBytes))
	require.NoError(t, err)
	require.Equal(t, meta["foc/own"], labels.Owner)
	require.Equal(t, meta["foc/ks"], labels.Keyspace)
	require.Equal(t, meta["foc/salt"], labels.Salt)
}

func TestReadEnvelopeLabelsRejectsCARTwoFiles(t *testing.T) {
	meta := validMeta()
	env := envelopeBytes(t, meta)
	carBytes := makeTwoFileCAR(t, encryptedFeeName, env, "other.bin", []byte("other"))

	_, err := ReadEnvelopeLabels(bytes.NewReader(carBytes))
	require.ErrorIs(t, err, ErrInvalidEnvelope)
	require.ErrorContains(t, err, "exactly one file")
}

func TestReadEnvelopeLabelsRejectsWrongCARFilename(t *testing.T) {
	meta := validMeta()
	env := envelopeBytes(t, meta)
	carBytes := makeNamedFileCAR(t, "plain.bin", env)

	_, err := ReadEnvelopeLabels(bytes.NewReader(carBytes))
	require.ErrorIs(t, err, ErrInvalidEnvelope)
	require.ErrorContains(t, err, encryptedFeeName)
}

func TestReadEnvelopeLabelsRejectsCARNonEnvelopePayload(t *testing.T) {
	carBytes := makeFileRootCAR(t, []byte("not an envelope at all"))

	_, err := ReadEnvelopeLabels(bytes.NewReader(carBytes))
	require.ErrorIs(t, err, ErrInvalidEnvelope)
}

type carBlock struct {
	c    cid.Cid
	data []byte
}

func makeFileRootCAR(t *testing.T, payload []byte) []byte {
	t.Helper()
	fileCID, fileData, leafCID, leafData := makeUnixFSFile(t, payload)
	return writeCAR(t, fileCID, []carBlock{
		{fileCID, fileData},
		{leafCID, leafData},
	})
}

func makeNamedFileCAR(t *testing.T, name string, payload []byte) []byte {
	t.Helper()
	fileCID, fileData, leafCID, leafData := makeUnixFSFile(t, payload)
	dirCID, dirData := makeUnixFSDir(t, name, fileCID, uint64(len(fileData)+len(leafData)))
	return writeCAR(t, dirCID, []carBlock{
		{dirCID, dirData},
		{fileCID, fileData},
		{leafCID, leafData},
	})
}

func makeTwoFileCAR(t *testing.T, nameA string, payloadA []byte, nameB string, payloadB []byte) []byte {
	t.Helper()
	fileA, fileAData, leafA, leafAData := makeUnixFSFile(t, payloadA)
	fileB, fileBData, leafB, leafBData := makeUnixFSFile(t, payloadB)
	dir := new(merkledag.ProtoNode)
	require.NoError(t, dir.SetCidBuilder(merkledag.V1CidPrefix()))
	fsn := unixfs.NewFSNode(unixfs.TDirectory)
	fsBytes, err := fsn.GetBytes()
	require.NoError(t, err)
	dir.SetData(fsBytes)
	require.NoError(t, dir.AddRawLink(nameA, &ipldformat.Link{Name: nameA, Size: uint64(len(fileAData) + len(leafAData)), Cid: fileA}))
	require.NoError(t, dir.AddRawLink(nameB, &ipldformat.Link{Name: nameB, Size: uint64(len(fileBData) + len(leafBData)), Cid: fileB}))
	return writeCAR(t, dir.Cid(), []carBlock{
		{dir.Cid(), dir.RawData()},
		{fileA, fileAData},
		{fileB, fileBData},
		{leafA, leafAData},
		{leafB, leafBData},
	})
}

func makeUnixFSFile(t *testing.T, payload []byte) (fileCID cid.Cid, fileData []byte, leafCID cid.Cid, leafData []byte) {
	t.Helper()
	leafCID, leafData = makeRawCID(t, payload)

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

func makeRawCID(t *testing.T, data []byte) (cid.Cid, []byte) {
	t.Helper()
	h, err := mh.Sum(data, mh.SHA2_256, -1)
	require.NoError(t, err)
	return cid.NewCidV1(cid.Raw, h), data
}

func writeCAR(t *testing.T, root cid.Cid, blocks []carBlock) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, car.WriteHeader(&car.CarHeader{Roots: []cid.Cid{root}, Version: 1}, &buf))
	for _, b := range blocks {
		require.NoError(t, util.LdWrite(&buf, b.c.Bytes(), b.data))
	}
	return buf.Bytes()
}
