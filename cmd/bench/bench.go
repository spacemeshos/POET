package main

import (
	"crypto/rand"
	"fmt"
	"github.com/spacemeshos/poet/prover"
	"github.com/spacemeshos/poet/shared"
	"github.com/spacemeshos/poet/verifier"
	"runtime"

	"log"
	"os"
	"path"
	"runtime/pprof"
	"time"
)

func main() {
	runtime.MemProfileRate = 0
	println("Memory profiling disabled.")

	cfg, err := loadConfig()
	if err != nil {
		os.Exit(1)
	}

	// Enable memory profiling
	if cfg.CPU {
		dir, err := os.Getwd()
		if err != nil {
			log.Fatal("cant get current dir", err)
		}

		profFilePath := path.Join(dir, "./CPU.prof")
		fmt.Printf("CPU profile: %s\n", profFilePath)

		f, err := os.Create(profFilePath)
		if err != nil {
			log.Fatal("could not create CPU profile: ", err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal("could not start CPU profile: ", err)
		}
		defer pprof.StopCPUProfile()

		println("Cpu profiling enabled and started...")
	}

	x := make([]byte, 20)
	_, err = rand.Read(x)
	if err != nil {
		panic("no entropy")
	}

	challenge := shared.Sha256Challenge(x)
	leafCount := uint64(1) << cfg.N
	securityParam := shared.T

	t1 := time.Now()
	println("Computing dag...")
	merkleProof, err := prover.GetProof(challenge, leafCount, securityParam)
	if err != nil {
		panic("failed to generate proof")
	}

	e := time.Since(t1)
	fmt.Printf("Proof generated in %s (%f)\n", e, e.Seconds())
	fmt.Printf("Dag root label: %x\n", merkleProof.Root)

	t1 = time.Now()
	err = verifier.Validate(merkleProof, challenge, leafCount, securityParam)
	if err != nil {
		panic("Failed to verify nip")
	}

	e = time.Since(t1)
	fmt.Printf("Proof verified in %s (%f)\n", e, e.Seconds())
}
