package planner

import (
	"fmt"

	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
)

// Capacities are conservative decimal native bytes. LA uses the 30 TB
// baseline because the barcode generation does not distinguish larger LTO-10
// media.
var nominalCapacityByGeneration = map[string]int64{
	"LTO-1":        100_000_000_000,
	"LTO-2":        200_000_000_000,
	"LTO-3":        400_000_000_000,
	"LTO-4":        800_000_000_000,
	"LTO-5":        1_500_000_000_000,
	"LTO-6":        2_500_000_000_000,
	"LTO-7":        6_000_000_000_000,
	"LTO-7 Type M": 9_000_000_000_000,
	"LTO-8":        12_000_000_000_000,
	"LTO-9":        18_000_000_000_000,
	"LTO-10":       30_000_000_000_000,
}

// NominalCapacity returns the conservative native capacity for a cartridge.
func NominalCapacity(id tape.ID) (int64, error) {
	generation := id.MediaGeneration()
	capacity, ok := nominalCapacityByGeneration[generation]
	if !ok {
		return 0, fmt.Errorf("planner: no nominal capacity for tape %q", id)
	}
	return capacity, nil
}

func largestNominalCapacity() int64 {
	var largest int64
	for _, capacity := range nominalCapacityByGeneration {
		if capacity > largest {
			largest = capacity
		}
	}
	return largest
}
