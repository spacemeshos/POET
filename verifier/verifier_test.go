package verifier

import (
	"github.com/spacemeshos/poet/hash"
	"github.com/spacemeshos/poet/prover"
	"github.com/spacemeshos/poet/shared"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"testing"
)

func TestValidate(t *testing.T) {
	r := require.New(t)
	tempdir, _ := ioutil.TempDir("", "poet-test")

	challenge := []byte("challenge")
	numLeaves := uint64(16)
	securityParam := uint8(4)
	merkleProof, err := prover.GenerateProofWithoutPersistency(tempdir, hash.GenLabelHashFunc(challenge), hash.GenMerkleHashFunc(challenge), numLeaves, securityParam, prover.LowestMerkleMinMemoryLayer)
	r.NoError(err)

	err = Validate(*merkleProof, hash.GenLabelHashFunc(challenge), hash.GenMerkleHashFunc(challenge), numLeaves, securityParam)
	r.NoError(err)
}

func TestValidateWrongSecParam(t *testing.T) {
	merkleProof := shared.MerkleProof{
		Root:         nil,
		ProvenLeaves: [][]byte{nil, nil},
		ProofNodes:   nil,
	}
	challenge := []byte("challenge")
	numLeaves := uint64(16)
	securityParam := uint8(4)
	err := Validate(merkleProof, hash.GenLabelHashFunc(challenge), hash.GenMerkleHashFunc(challenge), numLeaves, securityParam)
	require.EqualError(t, err, "number of proven leaves (2) must be equal to security param (4)")
}

func TestValidateWrongMerkleValidationError(t *testing.T) {
	merkleProof := shared.MerkleProof{
		Root:         nil,
		ProvenLeaves: [][]byte{},
		ProofNodes:   nil,
	}
	challenge := []byte("challenge")
	numLeaves := uint64(16)
	securityParam := uint8(0)
	err := Validate(merkleProof, hash.GenLabelHashFunc(challenge), hash.GenMerkleHashFunc(challenge), numLeaves, securityParam)
	require.EqualError(t, err, "error while validating merkle proof: at least one leaf is required for validation")
}

func TestValidateWrongRoot(t *testing.T) {
	r := require.New(t)
	tempdir, _ := ioutil.TempDir("", "poet-test")

	challenge := []byte("challenge")
	numLeaves := uint64(16)
	securityParam := uint8(4)
	merkleProof, err := prover.GenerateProofWithoutPersistency(tempdir, hash.GenLabelHashFunc(challenge), hash.GenMerkleHashFunc(challenge), numLeaves, securityParam, prover.LowestMerkleMinMemoryLayer)
	r.NoError(err)

	merkleProof.Root[0] = 0

	err = Validate(*merkleProof, hash.GenLabelHashFunc(challenge), hash.GenMerkleHashFunc(challenge), numLeaves, securityParam)
	r.EqualError(err, "merkle proof not valid")
}

func BadLabelHashFunc(data []byte) []byte {
	return []byte("not the right thing!")
}

func TestValidateFailLabelValidation(t *testing.T) {
	r := require.New(t)
	tempdir, _ := ioutil.TempDir("", "poet-test")

	challenge := []byte("challenge")
	numLeaves := uint64(16)
	securityParam := uint8(4)
	merkleProof, err := prover.GenerateProofWithoutPersistency(tempdir, hash.GenLabelHashFunc(challenge), hash.GenMerkleHashFunc(challenge), numLeaves, securityParam, prover.LowestMerkleMinMemoryLayer)
	r.NoError(err)

	err = Validate(*merkleProof, BadLabelHashFunc, hash.GenMerkleHashFunc(challenge), numLeaves, securityParam)
	r.Error(err)
	r.Regexp("label at index 0 incorrect - expected: [0-f]* actual: [0-f]*", err.Error())
}
