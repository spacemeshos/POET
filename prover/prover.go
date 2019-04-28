package prover

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/golang/groupcache/lru"
	"github.com/spacemeshos/poet/internal"
	"github.com/spacemeshos/poet/shared"
	"math"
	"os"
	"path"
	"runtime"
	"strings"
	"time"
)

const statsSampleSize = 100000

type Challenge = shared.Challenge
type Proof = shared.Proof
type IProver = shared.IProver
type IVerifier = shared.IBasicVerifier
type Identifier = shared.Identifier
type HashFunc = shared.HashFunc
type Label = shared.Label
type Labels = shared.Labels

type SMProver struct {
	x     []byte   // commitment
	n     uint     // n param 9 <= n <= 63
	h     HashFunc // Hx()
	f     internal.BinaryStringFactory
	phi   Label
	store internal.IKvStore
	cache *lru.Cache

	sb strings.Builder
	t  time.Time
}

// Create a new prover with commitment X and param 1 <= n <= 63
func New(x []byte, n uint, h HashFunc) (IProver, error) {

	if n < 9 || n > 63 {
		return nil, errors.New("n must be in (9, 63)")
	}

	res := &SMProver{
		x:     x,
		n:     n,
		h:     h,
		f:     internal.NewSMBinaryStringFactory(),
		cache: lru.New(int(n)), // we only need n entries in the labels cache
		sb:    strings.Builder{},
	}

	dir, err := os.Getwd()
	if err != nil {
		return res, err
	}

	d := make([]byte, 64)
	_, err = rand.Read(d)
	if err != nil {
		return res, err
	}

	fileName := fmt.Sprintf("./poet-%d.bin", binary.BigEndian.Uint64(d))
	log.Tracef("Dag store: %s", path.Join(dir, fileName))

	store, err := internal.NewKvFileStore(fileName, n)
	if err != nil {
		return res, err
	}

	err = store.Reset()
	if err != nil {
		return res, err
	}

	res.store = store

	return res, nil
}

func (p *SMProver) DeleteStore() {
	_ = p.store.Delete()
}

func (p *SMProver) GetLabel(id Identifier) (Label, bool) {

	inStore, err := p.store.IsLabelInStore(id)
	if err != nil {
		println(err)
		return Label{}, false
	}

	if !inStore {
		println("Warning: label for id %s not found in store", id)
		return Label{}, false
	}

	l, err := p.store.Read(id)
	if err != nil {
		println(err)
		return Label{}, false
	}

	return l, true
}

func (p *SMProver) GetHashFunction() HashFunc {
	return p.h
}

// Returns proof for a challenge
func (p *SMProver) GetProof(c Challenge) (Proof, error) {

	proof := Proof{}
	proof.Phi = p.phi
	proof.L = [shared.T]Labels{}

	// temp store use to ensure labels in proof are unique and not duplicated
	var m = make(map[Identifier]Label)

	// Iterate over each identifier in the challenge and create the proof for it
	for idx, id := range c.Data {

		var labels Labels

		bs, err := p.f.NewBinaryString(string(id))
		if err != nil {
			return Proof{}, err
		}

		// Add the label to the list only if it is not already included
		if _, ok := m[Identifier(id)]; !ok {
			// add the identifier's label to the labels list
			label := p.readLabel(Identifier(id))
			labels = append(labels, label)
			m[Identifier(id)] = label
		}

		siblingsIds, err := bs.GetBNSiblings(false)
		if err != nil {
			return Proof{}, err
		}

		for _, siblingId := range siblingsIds { // siblings ids up the path from the leaf to the root
			sibId := siblingId.GetStringValue()
			if _, ok := m[Identifier(sibId)]; !ok {

				// label is not already included in this proof

				// get its value
				sibLabel := p.readLabel(Identifier(sibId))

				// store it in m so we won't add it again in another labels list in the proof
				m[Identifier(sibId)] = sibLabel

				// add it to the list of labels in the proof for identifier id
				labels = append(labels, sibLabel)
			}
		}

		proof.L[idx] = labels
	}

	return proof, nil
}

// γ := (Hx(φ,1),...Hx(φ,t))
func (p *SMProver) CreateNipChallenge() (Challenge, error) {
	// use shared common func
	return internal.CreateNipChallenge(p.phi, p.h, p.n)
}

func (p *SMProver) GetNonInteractiveProof() (shared.Proof, error) {
	c, err := p.CreateNipChallenge()
	if err != nil {
		return shared.Proof{}, err
	}

	return p.GetProof(c)
}

func (p *SMProver) ComputeDag() (phi shared.Label, err error) {
	N := math.Pow(2, float64(p.n+1)) - 1
	L := math.Pow(2, float64(p.n))

	var msg string
	msg += fmt.Sprintf("DAG(%d):\n", p.n)
	msg += fmt.Sprintf("> Nodes: %d\n", uint64(N))
	msg += fmt.Sprintf("> Leaves: %d\n", uint64(L))
	msg += fmt.Sprintf("> Commitment: %x\n", p.x)
	msg += fmt.Sprintf("> Commitment hash: %x", p.h.Hash(p.x))
	log.Trace(msg)

	p.t = time.Now()

	rootLabel, err := p.computeDag(shared.RootIdentifier)
	if err != nil {
		return shared.Label{}, err
	}

	p.phi = rootLabel

	// Finalize writing w/o closing the file
	err = p.store.Finalize()
	if err != nil {
		return shared.Label{}, err
	}

	log.Tracef("Finalizing store. Wrote %d leaves. Total bytes: %d",
		p.store.Labels(), p.store.Bytes())

	return rootLabel, nil
}

// Compute dag rooted at node with identifier rootId
func (p *SMProver) computeDag(rootId shared.Identifier) (shared.Label, error) {
	p.sb.Reset()
	p.sb.WriteString(string(rootId))
	p.sb.WriteString("0")
	leftNodeId := shared.Identifier(p.sb.String())

	p.sb.Reset()
	p.sb.WriteString(string(rootId))
	p.sb.WriteString("1")
	rightNodId := shared.Identifier(p.sb.String())

	childrenHeight := uint(len(rootId)) + 1
	var leftNodeLabel, rightNodeLabel shared.Label
	var err error

	if childrenHeight == p.n { // children are leaves

		leftNodeLabel, err = p.computeLeafLabel(leftNodeId)
		p.cache.Add(string(leftNodeId), leftNodeLabel)
		if err != nil {
			return shared.Label{}, err
		}

		rightNodeLabel, err = p.computeLeafLabel(rightNodId)
		if err != nil {
			return shared.Label{}, err
		}

	} else { // children are internal dag nodes

		leftNodeLabel, err = p.computeDag(leftNodeId)

		// we cache left nodes as they are leaf parents
		p.cache.Add(string(leftNodeId), leftNodeLabel)

		if err != nil {
			return shared.Label{}, err
		}

		rightNodeLabel, err = p.computeDag(rightNodId)
		if err != nil {
			return shared.Label{}, err
		}
	}

	// Compute root label, store and return it
	rootLabelValue := p.h.Hash([]byte(rootId), leftNodeLabel, rightNodeLabel)

	p.store.Write(rootId, rootLabelValue)

	return rootLabelValue, nil
}

// Given a leaf node with id leafId - return the value of its label
// Pre-condition: all parent labels values have been computed and are available for the implementation
func (p *SMProver) computeLeafLabel(leafId shared.Identifier) (shared.Label, error) {
	bs, err := p.f.NewBinaryString(string(leafId))
	if err != nil {
		return shared.Label{}, err
	}

	// generate packed data to hash
	data := []byte(leafId)

	parentIds, err := bs.GetBNSiblings(true)
	if err != nil {
		return shared.Label{}, err
	}

	for _, parentId := range parentIds {

		// read all parent label values from the lru cache
		parentValue, ok := p.cache.Get(parentId.GetStringValue())
		if !ok {
			return shared.Label{}, errors.New("expected all parents labels in the lru cache")
		}

		var l = parentValue.(shared.Label)
		data = append(data, l...)
	}

	// note that the leftmost leaf has no parents in the dag
	label := p.h.Hash(data)

	// store it
	p.store.Write(leafId, label)

	// show stats
	if bs.GetValue()%statsSampleSize == 0 {
		freq := statsSampleSize / time.Since(p.t).Seconds()
		i := bs.GetValue()
		N := math.Pow(2, float64(p.n))
		r := math.Min(100.0, 100*float64(i)/N)
		log.Tracef("Leaf %s %d %.2v%% %0.2f leaves/sec", leafId, i, r, freq)
		p.t = time.Now()

		// PrintMemUsage()
	}

	return label, nil
}

// PrintMemUsage outputs the current, total and OS memory being used. As well as the number
// of garage collection cycles completed.
func PrintMemUsage() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	// For info on each, see: https://golang.org/pkg/runtime/#MemStats
	fmt.Printf("Alloc = %0.2f GiB", bToGb(m.Alloc))
	fmt.Printf("\tAllocObj = %v", m.Mallocs-m.Frees)
	fmt.Printf("\tHeapObjs = %v", m.HeapObjects)
	fmt.Printf("\tNumGC = %v\n", m.NumGC)

	fmt.Printf("Sys = %0.2f GiB\n", bToGb(m.Sys))
	fmt.Printf(" > HeapSys = %v MiB\n", bToMb(m.HeapSys))
	fmt.Printf(" > Stack = %v MiB\n", bToMb(m.StackSys))
	fmt.Printf(" > MSpan = %v MiB\n", bToMb(m.MSpanSys))
	fmt.Printf(" > MCache = %v MiB\n", bToMb(m.MCacheSys))
	fmt.Printf(" > Buckhash = %v MiB\n", bToMb(m.BuckHashSys))
	fmt.Printf(" > GC = %v MiB\n", bToMb(m.GCSys))
	fmt.Printf(" > Other = %v MiB\n\n", bToMb(m.OtherSys))

	// runtime.GC()
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}

func bToGb(b uint64) float64 {
	return float64(b) / 1024 / 1024 / 1024
}

func (p *SMProver) printDag(rootId shared.Identifier) {
	if rootId == "" {
		items := p.store.Size() / shared.WB
		fmt.Printf("DAG: # of nodes: %d. n: %d\n", items, p.n)
	}

	if uint(len(rootId)) < p.n {
		p.printDag(rootId + "0")
		p.printDag(rootId + "1")
	}

	ok, err := p.store.IsLabelInStore(rootId)

	if !ok || err != nil {
		fmt.Printf("Missing label value from map for is %s", rootId)
		return
	}

	label := p.readLabel(rootId)
	fmt.Printf("%s: %s\n", rootId, internal.GetDisplayValue(label))
}

func (p *SMProver) readLabel(id shared.Identifier) shared.Label {
	l, err := p.store.Read(id)
	if err != nil {
		println(err)
		panic(err)
	}

	return l
}
