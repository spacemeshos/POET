package internal

import (
	"errors"
	"github.com/spacemeshos/poet/shared"
	"math"
	"os"
)

type WriteData struct {
	id shared.Identifier
	l  shared.Label
}

type WriteChan chan *WriteData

type IKvStore interface {
	Read(id Identifier) (shared.Label, error)
	Write(id Identifier, l shared.Label)
	IsLabelInStore(id Identifier) (bool, error)
	Reset() error
	Delete() error
	Size() uint64
	Finalize() error // finalize writing w/o closing the file
	Close() error    // finalize and close

	Labels() uint64 // num of labels written to store in this session
	Bytes() uint64  // num of bytes written

	//GetWriteChan() WriteChan
}

type KVFileStore struct {
	fileName string
	file     *os.File
	n        uint // 9 <= n < 64
	f        BinaryStringFactory
	bw       *Writer

	c  uint64 // num of labels written to store in this session
	sz uint64 // num of bytes written
}

const buffSizeBytes = 1024 * 1024 * 1024

// Create a new prover with commitment X and 1 <= n < 64
// n specifies the leafs height from the root and the number of bits in leaf ids
func NewKvFileStore(fileName string, n uint) (IKvStore, error) {

	res := &KVFileStore{
		fileName: fileName,
		n:        n,
		f:        NewSMBinaryStringFactory(),
	}

	err := res.init()

	return res, err
}

func (d *KVFileStore) init() error {
	f, err := os.OpenFile(d.fileName, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return err
	}

	d.file = f

	// todo: compare pref w/o buffers
	d.bw = NewWriterSize(f, buffSizeBytes)

	return nil
}

func (d *KVFileStore) Write(id Identifier, l shared.Label) {
	// update stats
	d.c += 1
	d.sz += uint64(len(l))

	_, err := d.bw.Write(l)
	if err != nil {
		panic(err)
	}
}

// Removes all data from the file
func (d *KVFileStore) Reset() error {
	err := d.bw.Flush()
	if err != nil {
		return err
	}

	d.c = 0
	d.sz = 0
	return d.file.Truncate(0)
}

func (d *KVFileStore) Finalize() error {
	// flush buffered ata to the underling writer
	return d.bw.Flush()
}

func (d *KVFileStore) Close() error {
	err := d.Finalize()
	if err != nil {
		return err
	}

	return d.file.Close()
}

func (d *KVFileStore) Delete() error {
	return os.Remove(d.fileName)
}

func (d *KVFileStore) Size() uint64 {
	stats, err := d.file.Stat()
	if err != nil {
		println(err)
	}

	return uint64(stats.Size()) + uint64(d.bw.Buffered())
}

// Returns true iff node's label is in the store
func (d *KVFileStore) IsLabelInStore(id Identifier) (bool, error) {

	idx, err := d.calcFileIndex(id)
	if err != nil {
		return false, err
	}

	stats, err := d.file.Stat()
	if err != nil {
		return false, err
	}

	if d.bw.Buffered() > 0 && idx < (d.c*shared.WB) {
		// label is in file or in the buffer
		return true, nil
	}

	fileSize := uint64(stats.Size())
	return idx < fileSize, nil
}

// Read label value from the store
// Returns the label of node id or error if it is not in the store
func (d *KVFileStore) Read(id Identifier) (shared.Label, error) {

	label := make(shared.Label, shared.WB)

	// total # of labels written - # of buffered labels == idx of label at buff start
	// say 4 labels were written, and Buffered() is 64 bytes. 2 last labels
	// are in buffer and the index of the label at buff start is 2.
	// idAtBuffStart := d.c - uint64(d.bw.Buffered()/shared.WB)

	// label file index
	idx, err := d.calcFileIndex(id)
	if err != nil {
		return label, err
	}

	n, err := d.file.ReadAt(label, int64(idx))
	if err != nil {
		return label, err
	}

	if n == 0 {
		return label, errors.New("label for id is not in store")
	}

	return label, nil
}

// Returns the store file offset for the data of a node identified by id
func (d *KVFileStore) calcFileIndex(id Identifier) (uint64, error) {
	s := d.subtreeSize(len(id))
	s1, err := d.leftSiblingsSubtreeSize(id)
	if err != nil {
		return 0, err
	}

	idx := s + s1 - 1
	offset := idx * shared.WB
	//fmt.Printf("Node id %s. Index: %d. Offset: %d\n", id, idx, offset)
	return offset, nil
}

// Returns the size of the subtree rooted at a node at tree depth
func (d *KVFileStore) subtreeSize(depth int) uint64 {
	// Subtree height for a node at depth d
	h := d.n - uint(depth)
	return uint64(math.Pow(2, float64(h+1)) - 1)
}

// Returns the size of the subtrees rooted at left siblings on the path
// from node id to the root node
func (d *KVFileStore) leftSiblingsSubtreeSize(id Identifier) (uint64, error) {
	bs, err := d.f.NewBinaryString(string(id))
	if err != nil {
		return 0, err
	}

	siblings, err := bs.GetBNSiblings(true)
	if err != nil {
		return 0, err
	}

	var res uint64
	for _, s := range siblings {
		res += d.subtreeSize(int(s.GetDigitsCount()))
	}

	return res, nil
}

func (d *KVFileStore) Labels() uint64 {
	return d.c
}

func (d *KVFileStore) Bytes() uint64 {
	return d.sz
}
