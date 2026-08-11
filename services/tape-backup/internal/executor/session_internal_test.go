package executor

import (
	"testing"

	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/backupplan"
	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
)

const admissionPlanID = "01JAY7M2K8Q3V5N0X2W4Z6B8D1"

func TestAdmitPlanAcceptsInitShapeAndPinsAdmitVolume(t *testing.T) {
	const admittedVolume = volume.ID("01J8ZQ7W5TWHA6R6J8X4QZ9Y7V")
	plan := backupplan.Plan{
		PlanID: admissionPlanID,
		Target: backupplan.Target{
			TapeID:       "ABC123L6",
			MediumSerial: "MAM-ABC123-0001",
		},
		Actions: []backupplan.Action{
			{Type: backupplan.ActionReformat},
			{Type: backupplan.ActionAdmit, VolumeID: admittedVolume},
			{Type: backupplan.ActionBackup, MetadataRef: "tvdb:1", Generation: 1},
			{
				Type:      backupplan.ActionAssertInventory,
				Snapshots: []string{"OR3GIYR2GE.g1"},
			},
		},
	}

	got, err := admitPlan(plan)
	if err != nil {
		t.Fatalf("admitPlan error = %v", err)
	}
	if got.kind != planKindInit || got.pin != admittedVolume {
		t.Fatalf("admitPlan = %#v, want init pinned to %s", got, admittedVolume)
	}
}

func TestAdmitPlanRefusesDeferredActions(t *testing.T) {
	tests := []struct {
		name   string
		action backupplan.ActionType
	}{
		{name: "import", action: backupplan.ActionImport},
		{name: "verify", action: backupplan.ActionVerify},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := admitPlan(backupplan.Plan{
				PlanID: admissionPlanID,
				Target: backupplan.Target{
					TapeID: "ABC123L6",
				},
				Actions: []backupplan.Action{{Type: test.action}},
			})
			want := `executor: plan ` + admissionPlanID +
				` contains deferred action "` + string(test.action) + `"`
			if err == nil || err.Error() != want {
				t.Fatalf("admitPlan error = %v, want %q", err, want)
			}
		})
	}
}

func TestAdmitPlanRefusesReformatAgainstRecognizedVolumeShape(t *testing.T) {
	_, err := admitPlan(backupplan.Plan{
		PlanID: admissionPlanID,
		Target: backupplan.Target{
			TapeID:       "ABC123L6",
			MediumSerial: "MAM-ABC123-0001",
		},
		Actions: []backupplan.Action{
			{
				Type:     backupplan.ActionAssertVolume,
				VolumeID: "01J8ZQ7W5TWHA6R6J8X4QZ9Y7V",
			},
			{Type: backupplan.ActionReformat},
			{
				Type:     backupplan.ActionAdmit,
				VolumeID: "01J8ZQ7W5TWHA6R6J8X4QZ9Y7W",
			},
			{Type: backupplan.ActionAssertInventory, Snapshots: []string{}},
		},
	})
	const want = "executor: plan " + admissionPlanID +
		" reformat/admit actions do not match the init plan shape"
	if err == nil || err.Error() != want {
		t.Fatalf("admitPlan error = %v, want %q", err, want)
	}
}
