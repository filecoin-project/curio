package keysmithitest

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/lib/keysmith"
)

// synapse-sdk's default branch is master. There is no branch named main.
const (
	synapseRepo   = "https://github.com/JAG-UK/synapse-sdk.git"
	synapseBranch = "master"
)

// artifacts are a FEE object and grant presentations produced by keysmith.
type artifacts struct {
	Fee               string `json:"fee"`
	Presentation      string `json:"presentation"`
	OtherPresentation string `json:"otherPresentation"`
	PieceCID          string `json:"pieceCid"`
	OtherPieceCID     string `json:"otherPieceCid"`
	ChainID           string `json:"chainId"`
	Now               int64  `json:"now"`
	Owner             string `json:"owner"`
	Bearer            string `json:"bearer"`
	Keyspace          string `json:"keyspace"`
	Scope             string `json:"scope"`
	Salt              string `json:"salt"`
	Node              string `json:"node"`
}

func TestKeysmithInterop(t *testing.T) {
	// Setenv keeps this test out of the Go test cache: it tracks synapse-sdk master.
	t.Setenv("CURIO_KEYSMITH_ITEST", "1")
	art := produce(t)
	feeBytes := mustHex(t, art.Fee)
	presentation := mustHex(t, art.Presentation)
	other := mustHex(t, art.OtherPresentation)
	piece := mustHex(t, art.PieceCID)
	chainID, err := strconv.ParseUint(art.ChainID, 10, 64)
	require.NoError(t, err)
	now := time.Unix(art.Now, 0)
	network := new(big.Int).SetUint64(chainID)

	t.Run("fee", func(t *testing.T) {
		labels, err := keysmith.ReadEnvelopeLabels(bytes.NewReader(feeBytes))
		require.NoError(t, err)
		require.Equal(t, int64(1), labels.Version)
		require.Equal(t, art.Owner, strings.ToLower(labels.Owner))
		require.Equal(t, art.Keyspace, strings.ToLower(labels.Keyspace))
		require.True(t, labels.HasScope)
		require.Equal(t, art.Scope, labels.Scope)
		require.Equal(t, art.Salt, strings.ToLower(labels.Salt))

		fromPrefix, err := keysmith.LabelsFromPrefix(feeBytes)
		require.NoError(t, err)
		require.Equal(t, labels, fromPrefix)
	})

	t.Run("grant", func(t *testing.T) {
		labels, err := keysmith.ReadEnvelopeLabels(bytes.NewReader(feeBytes))
		require.NoError(t, err)
		claim, err := keysmith.VerifyPresentation(presentation, piece, labels.Piece(), network, now)
		require.NoError(t, err)
		require.Equal(t, art.Owner, strings.ToLower(claim.Owner.Hex()))
		require.Equal(t, art.Bearer, strings.ToLower(claim.Bearer.Hex()))
		require.Equal(t, art.Node, claim.Node)

		token := base64.RawURLEncoding.EncodeToString(presentation)
		req := httptest.NewRequest(http.MethodGet, "https://provider.example/piece", nil)
		req.Header.Set("Authorization", "FOC-Grant "+token)
		raw, err := keysmith.Credential(req)
		require.NoError(t, err)
		require.Equal(t, presentation, raw)
		_, err = keysmith.VerifyPresentation(raw, piece, labels.Piece(), network, now)
		require.NoError(t, err)
	})

	t.Run("rejects", func(t *testing.T) {
		labels, err := keysmith.ReadEnvelopeLabels(bytes.NewReader(feeBytes))
		require.NoError(t, err)
		pieceLabels := labels.Piece()
		checkGrant := func(raw []byte) error {
			_, err := keysmith.VerifyPresentation(raw, piece, pieceLabels, network, now)
			return err
		}

		_, err = keysmith.VerifyPresentation(other, piece, pieceLabels, network, now)
		require.ErrorContains(t, err, "another piece")
		_, err = keysmith.VerifyPresentation(other, mustHex(t, art.OtherPieceCID), pieceLabels, network, now)
		require.NoError(t, err)

		tampered := bytes.Clone(presentation)
		tampered[len(tampered)-1] ^= 0x01
		require.Error(t, checkGrant(tampered))

		require.ErrorIs(t, checkGrant(nil), keysmith.ErrInvalidGrant)
		require.ErrorIs(t, checkGrant([]byte("hello")), keysmith.ErrInvalidGrant)
		require.ErrorIs(t, checkGrant(bytes.Repeat([]byte{0xff}, 64)), keysmith.ErrInvalidGrant)
		require.ErrorIs(t, checkGrant(feeBytes), keysmith.ErrInvalidGrant)
		require.ErrorIs(t, checkGrant([]byte(`{"node":"keyspace","bearer":"0x0"}`)), keysmith.ErrInvalidGrant)

		junk, err := cbor.Marshal([]any{1, []any{"not a link"}, map[string]any{"ok": true}})
		require.NoError(t, err)
		require.ErrorIs(t, checkGrant(junk), keysmith.ErrInvalidGrant)

		_, err = keysmith.ReadEnvelopeLabels(bytes.NewReader(nil))
		require.ErrorIs(t, err, keysmith.ErrInvalidEnvelope)
		_, err = keysmith.ReadEnvelopeLabels(bytes.NewReader([]byte("not an envelope")))
		require.ErrorIs(t, err, keysmith.ErrInvalidEnvelope)
		_, err = keysmith.ReadEnvelopeLabels(bytes.NewReader(presentation))
		require.ErrorIs(t, err, keysmith.ErrInvalidEnvelope)
		_, err = keysmith.LabelsFromPrefix(bytes.Repeat([]byte{0x00}, 32))
		require.ErrorIs(t, err, keysmith.ErrInvalidEnvelope)
	})
}

// produce builds keysmith from synapse-sdk master and returns one FEE and two presentations.
func produce(t *testing.T) artifacts {
	t.Helper()
	root := checkout(t)
	build(t, root)

	script := filepath.Join(root, "packages", "keysmith", "curio-itest-produce.mjs")
	src, err := os.ReadFile(filepath.Join(moduleRoot(t), "itests", "keysmith", "produce.mjs"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(script, src, 0o644))

	cmd := exec.Command("node", script)
	cmd.Dir = filepath.Join(root, "packages", "keysmith")
	cmd.Env = commandEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), "keysmith producer failed\n%s", stderr.String())

	var art artifacts
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &art), "producer stdout\n%s\nstderr\n%s", stdout.String(), stderr.String())
	require.NotEmpty(t, art.Fee)
	require.NotEmpty(t, art.Presentation)
	require.NotEqual(t, art.Presentation, art.OtherPresentation)
	return art
}

// checkout returns a depth-1 clone of synapse-sdk at master, fetching when the clone already exists.
func checkout(t *testing.T) string {
	t.Helper()
	require.NoError(t, exec.Command("git", "version").Run(), "git is required")
	root := filepath.Join(os.TempDir(), "curio-itest-jag-synapse-sdk")
	if _, err := os.Stat(filepath.Join(root, ".git")); errors.Is(err, os.ErrNotExist) {
		run(t, "", "git", "clone", "--depth", "1", "--branch", synapseBranch, synapseRepo, root)
		return root
	}
	run(t, root, "git", "fetch", "--depth", "1", "origin", synapseBranch)
	run(t, root, "git", "checkout", "--force", "FETCH_HEAD")
	return root
}

// build installs and compiles keysmith and the FEE package when HEAD has changed.
func build(t *testing.T, root string) {
	t.Helper()
	head := strings.TrimSpace(string(run(t, root, "git", "rev-parse", "HEAD")))
	stamp := filepath.Join(root, ".curio-itest-stamp")
	prev, _ := os.ReadFile(stamp)
	dist := filepath.Join(root, "packages", "keysmith", "dist", "src", "index.js")
	if string(prev) == head {
		if _, err := os.Stat(dist); err == nil {
			return
		}
	}
	pnpm(t, root, "install", "--no-frozen-lockfile", "--filter", "@filoz/keysmith...", "--filter", "filecoin-encryption-envelope...")
	pnpm(t, root, "--dir", "packages/filecoin-encryption-envelope", "exec", "tsc", "--build", "--pretty")
	pnpm(t, root, "--dir", "packages/keysmith", "exec", "tsc", "--build", "--pretty")
	require.NoError(t, os.WriteFile(stamp, []byte(head), 0o644))
}

func pnpm(t *testing.T, dir string, args ...string) {
	t.Helper()
	argv := append([]string{"pnpm"}, args...)
	if _, err := exec.LookPath("corepack"); err == nil {
		argv = append([]string{"corepack"}, argv...)
	} else if _, err := exec.LookPath("pnpm"); err != nil {
		t.Fatal("pnpm is required (corepack enable, or a pnpm on PATH)")
	}
	run(t, dir, argv[0], argv[1:]...)
}

func run(t *testing.T, dir, name string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = commandEnv()
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s\n%s", cmd.String(), out)
	return out
}

func commandEnv() []string {
	return append(os.Environ(), "COREPACK_ENABLE_DOWNLOAD_PROMPT=0")
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}
