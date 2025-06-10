package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/filecoin-project/go-state-types/abi"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: piece-calc <command> <size>")
		fmt.Println("Commands:")
		fmt.Println("  pad <unpadded-size>    - Convert unpadded size to padded")
		fmt.Println("  unpad <padded-size>    - Convert padded size to unpadded")
		fmt.Println("  info <size>            - Show both padded and unpadded for input")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  piece-calc pad 1016")
		fmt.Println("  piece-calc unpad 1024") 
		fmt.Println("  piece-calc info 1024")
		os.Exit(1)
	}

	command := os.Args[1]
	sizeStr := os.Args[2]

	size, err := strconv.ParseUint(sizeStr, 10, 64)
	if err != nil {
		fmt.Printf("Error parsing size: %v\n", err)
		os.Exit(1)
	}

	switch command {
	case "pad":
		unpadded := abi.UnpaddedPieceSize(size)
		padded := unpadded.Padded()
		fmt.Printf("Unpadded: %d bytes\n", unpadded)
		fmt.Printf("Padded:   %d bytes\n", padded)
		fmt.Printf("Ratio:    %.4f (padded/unpadded)\n", float64(padded)/float64(unpadded))

	case "unpad":
		padded := abi.PaddedPieceSize(size)
		unpadded := padded.Unpadded()
		fmt.Printf("Padded:   %d bytes\n", padded)
		fmt.Printf("Unpadded: %d bytes\n", unpadded)
		fmt.Printf("Ratio:    %.4f (padded/unpadded)\n", float64(padded)/float64(unpadded))

	case "info":
		// Try both interpretations
		fmt.Printf("Input size: %d bytes\n\n", size)
		
		fmt.Println("If input is UNPADDED:")
		unpadded := abi.UnpaddedPieceSize(size)
		padded := unpadded.Padded()
		fmt.Printf("  Unpadded: %d bytes\n", unpadded)
		fmt.Printf("  Padded:   %d bytes\n", padded)
		fmt.Printf("  Ratio:    %.4f\n", float64(padded)/float64(unpadded))

		fmt.Println("\nIf input is PADDED:")
		paddedInput := abi.PaddedPieceSize(size)
		unpaddedOutput := paddedInput.Unpadded()
		fmt.Printf("  Padded:   %d bytes\n", paddedInput)
		fmt.Printf("  Unpadded: %d bytes\n", unpaddedOutput)
		fmt.Printf("  Ratio:    %.4f\n", float64(paddedInput)/float64(unpaddedOutput))

		// Show common piece sizes for reference
		fmt.Println("\nCommon piece sizes:")
		commonSizes := []uint64{127, 254, 508, 1016, 2032, 4064, 8128, 16256, 32512, 65024}
		for _, cs := range commonSizes {
			up := abi.UnpaddedPieceSize(cs)
			p := up.Padded()
			fmt.Printf("  %d -> %d\n", up, p)
		}

	default:
		fmt.Printf("Unknown command: %s\n", command)
		os.Exit(1)
	}
}