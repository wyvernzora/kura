package render

import (
	"bytes"
	"testing"

	tapeapi "github.com/wyvernzora/kura/services/tape-backup/pkg/api"
)

func TestTapePlanInitExactOutput(t *testing.T) {
	metadataRef := "tvdb:100"
	generation := 7
	volumeID := "01KTESTVOLUME0000000000000"
	result := tapeapi.PlanResult{
		Classification: "init",
		Persisted:      true,
		Plan: tapeapi.Plan{
			PlanID: "01KTESTPLAN000000000000000",
			Target: tapeapi.PlanTarget{
				TapeID:       "BLK001L6",
				MediumSerial: "MAM-SERIAL-1",
			},
			Actions: []tapeapi.Action{
				{Type: "reformat"},
				{Type: "admit", VolumeID: &volumeID},
				{
					Type:        "backup",
					MetadataRef: &metadataRef,
					Generation:  &generation,
				},
			},
		},
	}
	var output bytes.Buffer
	if err := TapePlan(&output, result, false); err != nil {
		t.Fatalf("TapePlan() error = %v", err)
	}
	const want = "Classification: init\n" +
		"Plan: 01KTESTPLAN000000000000000\n" +
		"Tape: BLK001L6\n" +
		"Actions:\n" +
		"  reformat\n" +
		"  admit volume=01KTESTVOLUME0000000000000\n" +
		"  backup tvdb:100 generation=7\n" +
		"Series:\n" +
		"  tvdb:100\n" +
		"Attestation: no readable identity, serial MAM-SERIAL-1; init will format this cartridge.\n" +
		"Approval: required — run `kura tape approve 01KTESTPLAN000000000000000`.\n"
	if output.String() != want {
		t.Fatalf("TapePlan() output = %q, want %q", output.String(), want)
	}
}

func TestTapeRunExactSeries(t *testing.T) {
	first := "tvdb:100"
	second := "tvdb:200"
	result := tapeapi.RunResult{
		Classification: "fill",
		Plan: tapeapi.Plan{
			PlanID: "01KTESTPLAN000000000000000",
			Target: tapeapi.PlanTarget{TapeID: "ABC123L6"},
			Actions: []tapeapi.Action{
				{Type: "backup", MetadataRef: &first},
				{Type: "backup", MetadataRef: &second},
			},
		},
	}
	var output bytes.Buffer
	if err := TapeRun(&output, result, false); err != nil {
		t.Fatalf("TapeRun() error = %v", err)
	}
	const want = "Executed fill plan 01KTESTPLAN000000000000000 on ABC123L6.\n" +
		"Series:\n" +
		"  tvdb:100\n" +
		"  tvdb:200\n"
	if output.String() != want {
		t.Fatalf("TapeRun() output = %q, want %q", output.String(), want)
	}
}
