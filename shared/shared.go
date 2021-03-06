package shared

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/spacemeshos/merkle-tree"
	"github.com/spacemeshos/sha256-simd"
	"os"
	"time"
)

const (
	// T is the security param which determines the number of leaves
	// to be included in a non-interactive proof.
	T uint8 = 150

	// OwnerReadWrite is a standard owner read / write file permission.
	OwnerReadWrite = os.FileMode(0600)
)

// FiatShamir generates a set of indices to include in a non-interactive proof.
func FiatShamir(challenge []byte, spaceSize uint64, securityParam uint8) map[uint64]bool {
	if uint64(securityParam) > spaceSize {
		securityParam = uint8(spaceSize)
	}
	ret := make(map[uint64]bool, securityParam)
	for i := uint8(0); len(ret) < int(securityParam); i++ {
		result := sha256.Sum256(append(challenge, i))
		id := binary.BigEndian.Uint64(result[:8]) % spaceSize
		ret[id] = true
	}
	return ret
}

// MakeLabelFunc returns a function which generates a PoET DAG label by concatenating a representation
// of the labelID with the list of left siblings and then hashing the result using the provided hash function.
//
// ⚠️ The resulting function is NOT thread-safe, however different generated instances are independent.
// The code is optimized for performance and memory allocations.
func MakeLabelFunc() func(hash func(data []byte) []byte, labelID uint64, leftSiblings [][]byte) []byte {
	var buffer []byte
	return func(hash func(data []byte) []byte, labelID uint64, leftSiblings [][]byte) []byte {
		// Calculate the buffer required size.
		// 8 is for the size of labelID.
		// leftSiblings slice might contain nil values, so the result size is inflated and used as an upper bound.
		size := 8 + len(leftSiblings)*merkle.NodeSize

		if len(buffer) < size {
			buffer = make([]byte, size)
		}

		binary.BigEndian.PutUint64(buffer, labelID)
		offset := 8

		for _, sibling := range leftSiblings {
			copied := copy(buffer[offset:], sibling)
			offset += copied
		}
		sum := hash(buffer[:offset])
		return sum
	}
}

type MerkleProof struct {
	Root         []byte
	ProvenLeaves [][]byte
	ProofNodes   [][]byte
}

// Retry provides generic capability for retryable function execution.
func Retry(retryable func() error, numRetries int, interval time.Duration, logger func(msg string)) error {
	err := retryable()
	if err == nil {
		return nil
	}

	if numRetries < 1 {
		logger(err.Error())
		return errors.New("number of retries exceeded")
	}

	logger(fmt.Sprintf("%v | retrying in %v...", err, interval))
	time.Sleep(interval)

	return Retry(retryable, numRetries-1, interval, logger)
}
