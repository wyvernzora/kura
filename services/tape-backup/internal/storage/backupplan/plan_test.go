package backupplan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
)

const (
	testPlanID      = "01JAY7M2K8Q3V5N0X2W4Z6B8D1"
	secondPlanID    = "01JAY8B4N1R6X8Q0Z2C4E6G8J3"
	testSessionID   = "01JAYC3P5R7T9W1Y3A5C7E9G2K"
	testVolumeID    = "01J8ZQ7W5TWHA6R6J8X4QZ9Y7V"
	secondVolumeID  = "01J8ZQ7W5TWHA6R6J8X4QZ9Y7W"
	testSnapshot    = "OR3GIYR2GM3TAMBXGA.g7"
	secondSnapshot  = "MFSGIYTONRSGSZDBOI.g2"
	testFingerprint = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func TestPlanRoundTripAndLifecycle(t *testing.T) {
	root := t.TempDir()
	plan := validPlan()

	if err := Draft(root, plan); err != nil {
		t.Fatalf("Draft: %v", err)
	}
	got, err := ReadDraft(root, plan.PlanID)
	if err != nil {
		t.Fatalf("ReadDraft: %v", err)
	}
	if !reflect.DeepEqual(got, plan) {
		t.Fatalf("ReadDraft = %#v, want %#v", got, plan)
	}
	assertPlanLists(t, root, 1, 0, 0)
	assertPlanOnlyAt(t, root, plan.PlanID, stateDraft)

	if err := Approve(root, plan.PlanID); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	got, err = ReadReady(root, plan.PlanID)
	if err != nil {
		t.Fatalf("ReadReady: %v", err)
	}
	if !reflect.DeepEqual(got, plan) {
		t.Fatalf("ReadReady = %#v, want %#v", got, plan)
	}
	assertPlanLists(t, root, 0, 1, 0)
	assertPlanOnlyAt(t, root, plan.PlanID, stateReady)

	if err := Complete(root, plan.PlanID); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	got, err = ReadDone(root, plan.PlanID)
	if err != nil {
		t.Fatalf("ReadDone: %v", err)
	}
	if !reflect.DeepEqual(got, plan) {
		t.Fatalf("ReadDone = %#v, want %#v", got, plan)
	}
	assertPlanLists(t, root, 0, 0, 1)
	assertPlanOnlyAt(t, root, plan.PlanID, stateDone)

	data, err := os.ReadFile(planFile(root, stateDone, plan.PlanID))
	if err != nil {
		t.Fatalf("ReadFile done plan: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("Unmarshal plan: %v", err)
	}
	if got, want := wire["schemaVersion"], float64(1); got != want {
		t.Fatalf("schemaVersion = %#v, want %#v", got, want)
	}
	if got, want := wire["planID"], plan.PlanID; got != want {
		t.Fatalf("planID = %#v, want %#v", got, want)
	}
}

func TestPlanTypesClosedEnum(t *testing.T) {
	for _, planType := range []PlanType{
		PlanTypeAdmit,
		PlanTypeBackup,
		PlanTypeImport,
		PlanTypeVerify,
	} {
		t.Run(string(planType), func(t *testing.T) {
			plan := validPlan()
			plan.Type = planType
			if err := Draft(t.TempDir(), plan); err != nil {
				t.Fatalf("Draft: %v", err)
			}
		})
	}
}

func TestPlanValidationOnEncodeAndDecode(t *testing.T) {
	cases := []struct {
		name       string
		mutatePlan func(*Plan)
		mutateWire func(*planWire)
		want       string
	}{
		{
			name: "planID required",
			mutatePlan: func(plan *Plan) {
				plan.PlanID = ""
			},
			mutateWire: func(wire *planWire) {
				wire.PlanID = ""
			},
			want: "backupplan: planID is required",
		},
		{
			name: "planID canonical ULID",
			mutatePlan: func(plan *Plan) {
				plan.PlanID = strings.ToLower(testPlanID)
			},
			mutateWire: func(wire *planWire) {
				wire.PlanID = strings.ToLower(testPlanID)
			},
			want: `backupplan: planID "01jay7m2k8q3v5n0x2w4z6b8d1" must be a ` +
				"26-character uppercase Crockford base32 ULID",
		},
		{
			name: "type",
			mutatePlan: func(plan *Plan) {
				plan.Type = "compact"
			},
			mutateWire: func(wire *planWire) {
				wire.Type = "compact"
			},
			want: `backupplan: unsupported plan type "compact"`,
		},
		{
			name: "createdAt required",
			mutatePlan: func(plan *Plan) {
				plan.CreatedAt = time.Time{}
			},
			mutateWire: func(wire *planWire) {
				wire.CreatedAt = ""
			},
			want: "backupplan: createdAt is required",
		},
		{
			name: "createdAt UTC",
			mutatePlan: func(plan *Plan) {
				plan.CreatedAt = time.Date(
					2026, 7, 27, 2, 14, 22, 0,
					time.FixedZone("PDT", -7*60*60),
				)
			},
			mutateWire: func(wire *planWire) {
				wire.CreatedAt = "2026-07-27T02:14:22-07:00"
			},
			want: "backupplan: createdAt must be UTC",
		},
		{
			name: "createdAt whole seconds",
			mutatePlan: func(plan *Plan) {
				plan.CreatedAt = plan.CreatedAt.Add(time.Nanosecond)
			},
			mutateWire: func(wire *planWire) {
				wire.CreatedAt = "2026-07-27T09:14:22.000000001Z"
			},
			want: "backupplan: createdAt must be truncated to whole seconds",
		},
		{
			name: "createdBy version required",
			mutatePlan: func(plan *Plan) {
				plan.CreatedBy.Version = ""
			},
			mutateWire: func(wire *planWire) {
				wire.CreatedBy.Version = ""
			},
			want: "backupplan: createdBy.version is required",
		},
		{
			name: "createdBy host required",
			mutatePlan: func(plan *Plan) {
				plan.CreatedBy.Host = ""
			},
			mutateWire: func(wire *planWire) {
				wire.CreatedBy.Host = ""
			},
			want: "backupplan: createdBy.host is required",
		},
		{
			name: "volumeID",
			mutatePlan: func(plan *Plan) {
				plan.Target.VolumeID = "not-a-volume"
			},
			mutateWire: func(wire *planWire) {
				wire.Target.VolumeID = "not-a-volume"
			},
			want: `backupplan: volumeID "not-a-volume" must be a 26-character ` +
				"uppercase Crockford base32 ULID",
		},
		{
			name: "tapeID",
			mutatePlan: func(plan *Plan) {
				plan.Target.TapeID = "bad"
			},
			mutateWire: func(wire *planWire) {
				wire.Target.TapeID = "bad"
			},
			want: `backupplan: tapeID "bad" must be exactly 8 characters`,
		},
		{
			name: "requiredFreeBytes negative",
			mutatePlan: func(plan *Plan) {
				plan.Target.RequiredFreeBytes = -1
			},
			mutateWire: func(wire *planWire) {
				wire.Target.RequiredFreeBytes = -1
			},
			want: "backupplan: requiredFreeBytes must not be negative",
		},
		{
			name: "expected snapshot canonical",
			mutatePlan: func(plan *Plan) {
				plan.Target.ExpectedSnapshots[0] = strings.ToLower(testSnapshot)
			},
			mutateWire: func(wire *planWire) {
				wire.Target.ExpectedSnapshots[0] = strings.ToLower(testSnapshot)
			},
			want: `backupplan: expected snapshot "or3giyr2gm3tambxga.g7": snapshot name ` +
				`"or3giyr2gm3tambxga.g7" has invalid base32 metadataRef: ` +
				"illegal base32 data at input byte 0",
		},
		{
			name: "duplicate expected snapshot",
			mutatePlan: func(plan *Plan) {
				plan.Target.ExpectedSnapshots = append(
					plan.Target.ExpectedSnapshots,
					plan.Target.ExpectedSnapshots[0],
				)
			},
			mutateWire: func(wire *planWire) {
				wire.Target.ExpectedSnapshots = append(
					wire.Target.ExpectedSnapshots,
					wire.Target.ExpectedSnapshots[0],
				)
			},
			want: `backupplan: duplicate expected snapshot "OR3GIYR2GM3TAMBXGA.g7"`,
		},
		{
			name: "item snapshot already expected",
			mutatePlan: func(plan *Plan) {
				plan.Target.ExpectedSnapshots = append(
					plan.Target.ExpectedSnapshots,
					"OR3GIYR2GM3TAMBXGA.g8",
				)
			},
			mutateWire: func(wire *planWire) {
				wire.Target.ExpectedSnapshots = append(
					wire.Target.ExpectedSnapshots,
					"OR3GIYR2GM3TAMBXGA.g8",
				)
			},
			want: `backupplan: item ("tvdb:370070", 8) snapshot ` +
				`"OR3GIYR2GM3TAMBXGA.g8" already exists on target`,
		},
		{
			name: "items required",
			mutatePlan: func(plan *Plan) {
				plan.Items = nil
				plan.Target.RequiredFreeBytes = 0
			},
			mutateWire: func(wire *planWire) {
				wire.Items = nil
				wire.Target.RequiredFreeBytes = 0
			},
			want: "backupplan: items must contain at least one item",
		},
		{
			name: "metadataRef required",
			mutatePlan: func(plan *Plan) {
				plan.Items[0].MetadataRef = ""
			},
			mutateWire: func(wire *planWire) {
				wire.Items[0].MetadataRef = ""
			},
			want: `backupplan: item ("", 8): metadataRef is required`,
		},
		{
			name: "generation positive",
			mutatePlan: func(plan *Plan) {
				plan.Items[0].Generation = 0
			},
			mutateWire: func(wire *planWire) {
				wire.Items[0].Generation = 0
			},
			want: `backupplan: item ("tvdb:370070", 0): generation must be at least 1`,
		},
		{
			name: "title required",
			mutatePlan: func(plan *Plan) {
				plan.Items[0].Title = ""
			},
			mutateWire: func(wire *planWire) {
				wire.Items[0].Title = ""
			},
			want: "backupplan: item title is required",
		},
		{
			name: "fingerprint format",
			mutatePlan: func(plan *Plan) {
				plan.Items[0].PayloadFingerprint = "bad"
			},
			mutateWire: func(wire *planWire) {
				wire.Items[0].PayloadFingerprint = "bad"
			},
			want: "backupplan: payloadFingerprint must use algorithm:digest format",
		},
		{
			name: "fingerprint algorithm",
			mutatePlan: func(plan *Plan) {
				plan.Items[0].PayloadFingerprint = "sha512:" + strings.Repeat("a", 64)
			},
			mutateWire: func(wire *planWire) {
				wire.Items[0].PayloadFingerprint = "sha512:" + strings.Repeat("a", 64)
			},
			want: `backupplan: unsupported payloadFingerprint algorithm "sha512"`,
		},
		{
			name: "fingerprint length",
			mutatePlan: func(plan *Plan) {
				plan.Items[0].PayloadFingerprint = "sha256:short"
			},
			mutateWire: func(wire *planWire) {
				wire.Items[0].PayloadFingerprint = "sha256:short"
			},
			want: "backupplan: payloadFingerprint sha256 digest must be 64 " +
				"lowercase hexadecimal characters",
		},
		{
			name: "fingerprint lowercase",
			mutatePlan: func(plan *Plan) {
				plan.Items[0].PayloadFingerprint = "sha256:" + strings.Repeat("A", 64)
			},
			mutateWire: func(wire *planWire) {
				wire.Items[0].PayloadFingerprint = "sha256:" + strings.Repeat("A", 64)
			},
			want: "backupplan: payloadFingerprint sha256 digest must be 64 " +
				"lowercase hexadecimal characters",
		},
		{
			name: "item bytes negative",
			mutatePlan: func(plan *Plan) {
				plan.Items[0].Bytes = -1
			},
			mutateWire: func(wire *planWire) {
				wire.Items[0].Bytes = -1
			},
			want: `backupplan: bytes for ("tvdb:370070", 8) must not be negative`,
		},
		{
			name: "duplicate item",
			mutatePlan: func(plan *Plan) {
				plan.Items[1].MetadataRef = plan.Items[0].MetadataRef
				plan.Items[1].Generation = plan.Items[0].Generation
			},
			mutateWire: func(wire *planWire) {
				wire.Items[1].MetadataRef = wire.Items[0].MetadataRef
				wire.Items[1].Generation = wire.Items[0].Generation
			},
			want: `backupplan: duplicate item ("tvdb:370070", 8)`,
		},
		{
			name: "requiredFreeBytes sum",
			mutatePlan: func(plan *Plan) {
				plan.Target.RequiredFreeBytes++
			},
			mutateWire: func(wire *planWire) {
				wire.Target.RequiredFreeBytes++
			},
			want: "backupplan: requiredFreeBytes is 31, want sum of item bytes 30",
		},
		{
			name: "requiredFreeBytes overflow",
			mutatePlan: func(plan *Plan) {
				plan.Items[0].Bytes = math.MaxInt64
				plan.Items[1].Bytes = 1
				plan.Target.RequiredFreeBytes = math.MaxInt64
			},
			mutateWire: func(wire *planWire) {
				wire.Items[0].Bytes = math.MaxInt64
				wire.Items[1].Bytes = 1
				wire.Target.RequiredFreeBytes = math.MaxInt64
			},
			want: "backupplan: requiredFreeBytes overflow",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertPlanEncodeAndDecodeRejection(
				t,
				tc.mutatePlan,
				tc.mutateWire,
				tc.want,
			)
		})
	}
}

func TestPlanRoundTripPreservesBlankCartridgeExpectedSnapshots(t *testing.T) {
	root := t.TempDir()
	plan := validPlan()
	plan.Target.ExpectedSnapshots = nil
	if err := Draft(root, plan); err != nil {
		t.Fatalf("Draft: %v", err)
	}
	got, err := ReadDraft(root, plan.PlanID)
	if err != nil {
		t.Fatalf("ReadDraft: %v", err)
	}
	if !reflect.DeepEqual(got, plan) {
		t.Fatalf("ReadDraft = %#v, want %#v", got, plan)
	}
}

func TestPlanTextValidationOnEncode(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Plan)
		want   string
	}{
		{
			name: "createdBy version valid UTF-8",
			mutate: func(plan *Plan) {
				plan.CreatedBy.Version = "v0.\xff.0"
			},
			want: "backupplan: createdBy.version must be valid UTF-8",
		},
		{
			name: "createdBy host valid UTF-8",
			mutate: func(plan *Plan) {
				plan.CreatedBy.Host = "control-\xff"
			},
			want: "backupplan: createdBy.host must be valid UTF-8",
		},
		{
			name: "title valid UTF-8",
			mutate: func(plan *Plan) {
				plan.Items[0].Title = "Sh\xffw"
			},
			want: "backupplan: item title must be valid UTF-8",
		},
		{
			name: "metadataRef valid UTF-8",
			mutate: func(plan *Plan) {
				plan.Items[0].MetadataRef = "tvdb:\xff"
			},
			want: `backupplan: item ("tvdb:\xff", 8): metadataRef must be valid UTF-8`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := validPlan()
			tc.mutate(&plan)
			err := Draft(t.TempDir(), plan)
			assertExactError(t, err, tc.want)
		})
	}
}

func TestPlanDecodeRejectsRawInvalidUTF8InEveryTextField(t *testing.T) {
	cases := []struct {
		name string
		old  string
		new  []byte
	}{
		{name: "createdBy version", old: "v0.6.0", new: []byte("v0.\xff.0")},
		{name: "createdBy host", old: "kura-control-0", new: []byte("kura-\xffontrol-0")},
		{name: "title", old: "Show", new: []byte("Sh\xffw")},
		{name: "metadataRef", old: "tvdb:370070", new: []byte("tvdb:\xff70070")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(toWire(validPlan()))
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			data = bytes.Replace(data, []byte(tc.old), tc.new, 1)
			_, err = decodePlan(data, testPlanID)
			assertExactError(t, err, "backupplan: plan must be valid UTF-8")
		})
	}
}

func TestPlanSchemaVersionGate(t *testing.T) {
	wire := toWire(validPlan())
	wire.SchemaVersion = 2
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	_, err = decodePlan(data, testPlanID)
	assertExactError(t, err, "backupplan: unsupported plan schemaVersion 2")
}

func TestPlanIDMustMatchFilename(t *testing.T) {
	root := t.TempDir()
	plan := validPlan()
	if err := Draft(root, plan); err != nil {
		t.Fatalf("Draft: %v", err)
	}
	data, err := os.ReadFile(planFile(root, stateDraft, plan.PlanID))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := os.WriteFile(planFile(root, stateDraft, secondPlanID), data, 0o664); err != nil {
		t.Fatalf("WriteFile copied plan: %v", err)
	}

	_, err = ReadDraft(root, secondPlanID)
	want := fmt.Sprintf(
		"backupplan: planID mismatch: filename is %q, plan contains %q",
		secondPlanID,
		testPlanID,
	)
	assertExactError(t, err, want)
}

func TestOneLivePlanPerVolume(t *testing.T) {
	for _, existingState := range []planState{stateDraft, stateReady} {
		t.Run(string(existingState), func(t *testing.T) {
			root := t.TempDir()
			first := validPlan()
			if err := Draft(root, first); err != nil {
				t.Fatalf("Draft first: %v", err)
			}
			if existingState == stateReady {
				if err := Approve(root, first.PlanID); err != nil {
					t.Fatalf("Approve first: %v", err)
				}
			}
			second := validPlan()
			second.PlanID = secondPlanID

			err := Draft(root, second)
			want := fmt.Sprintf(
				"backupplan: volume %s already has live plan %s",
				testVolumeID,
				testPlanID,
			)
			assertExactError(t, err, want)
			assertNotExist(t, planFile(root, stateDraft, secondPlanID))
		})
	}
}

func TestCompletedPlanDoesNotBlockNewPlanForVolume(t *testing.T) {
	root := t.TempDir()
	first := validPlan()
	if err := Draft(root, first); err != nil {
		t.Fatalf("Draft first: %v", err)
	}
	if err := Approve(root, first.PlanID); err != nil {
		t.Fatalf("Approve first: %v", err)
	}
	if err := Complete(root, first.PlanID); err != nil {
		t.Fatalf("Complete first: %v", err)
	}
	second := validPlan()
	second.PlanID = secondPlanID
	if err := Draft(root, second); err != nil {
		t.Fatalf("Draft second: %v", err)
	}
}

func TestDraftSkipsNonPlanJSONWhileCheckingLiveVolumes(t *testing.T) {
	root := t.TempDir()
	draftDir := planStateDir(root, stateDraft)
	if err := os.MkdirAll(draftDir, 0o775); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(draftDir, "notes.json"),
		[]byte(`{"hi":1}`),
		0o664,
	); err != nil {
		t.Fatalf("WriteFile notes: %v", err)
	}
	if err := Draft(root, validPlan()); err != nil {
		t.Fatalf("Draft: %v", err)
	}
}

func TestDraftNamesCorruptLivePlanPath(t *testing.T) {
	root := t.TempDir()
	path := planFile(root, stateDraft, secondPlanID)
	if err := os.MkdirAll(filepath.Dir(path), 0o775); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o664); err != nil {
		t.Fatalf("WriteFile corrupt plan: %v", err)
	}

	err := Draft(root, validPlan())
	want := fmt.Sprintf(
		"backupplan: draft plan %s: inspect live draft plan %q: "+
			"backupplan: decode plan %s: unexpected end of JSON input",
		testPlanID,
		path,
		secondPlanID,
	)
	assertExactError(t, err, want)
}

func TestPlanMutationsRejectLifecycleDirectorySymlinks(t *testing.T) {
	t.Run("draft", func(t *testing.T) {
		root := t.TempDir()
		readyDir := planStateDir(root, stateReady)
		if err := os.MkdirAll(readyDir, 0o775); err != nil {
			t.Fatalf("MkdirAll ready: %v", err)
		}
		draftDir := planStateDir(root, stateDraft)
		if err := os.Symlink(readyDir, draftDir); err != nil {
			t.Fatalf("Symlink draft: %v", err)
		}

		err := Draft(root, validPlan())
		want := fmt.Sprintf(
			"backupplan: draft plan directory %q is not a directory",
			draftDir,
		)
		assertExactError(t, err, want)
		assertNotExist(t, planFile(root, stateReady, testPlanID))
	})

	t.Run("move", func(t *testing.T) {
		root := t.TempDir()
		plan := validPlan()
		if err := Draft(root, plan); err != nil {
			t.Fatalf("Draft: %v", err)
		}
		readyDir := planStateDir(root, stateReady)
		doneDir := planStateDir(root, stateDone)
		if err := os.MkdirAll(doneDir, 0o775); err != nil {
			t.Fatalf("MkdirAll done: %v", err)
		}
		if err := os.Symlink(doneDir, readyDir); err != nil {
			t.Fatalf("Symlink ready: %v", err)
		}

		err := Approve(root, plan.PlanID)
		want := fmt.Sprintf(
			"backupplan: ready plan directory %q is not a directory",
			readyDir,
		)
		assertExactError(t, err, want)
		if _, err := ReadDraft(root, plan.PlanID); err != nil {
			t.Fatalf("ReadDraft after rejected move: %v", err)
		}
	})
}

func TestReadRejectsPlanInMultipleStates(t *testing.T) {
	root := t.TempDir()
	plan := validPlan()
	if err := Draft(root, plan); err != nil {
		t.Fatalf("Draft: %v", err)
	}
	data, err := os.ReadFile(planFile(root, stateDraft, plan.PlanID))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := os.MkdirAll(planStateDir(root, stateDone), 0o775); err != nil {
		t.Fatalf("MkdirAll done: %v", err)
	}
	if err := os.WriteFile(planFile(root, stateDone, plan.PlanID), data, 0o664); err != nil {
		t.Fatalf("WriteFile duplicate: %v", err)
	}

	_, err = ReadDraft(root, plan.PlanID)
	want := fmt.Sprintf(
		"backupplan: plan %s exists in multiple states: draft, done",
		plan.PlanID,
	)
	assertExactError(t, err, want)
}

func TestApproveRefusesOccupiedFileDestinationWithoutReplacingIt(t *testing.T) {
	testMoveRefusesOccupiedDestination(t, stateDraft, stateReady, Approve)
}

func TestCompleteRefusesOccupiedFileDestinationWithoutReplacingIt(t *testing.T) {
	testMoveRefusesOccupiedDestination(t, stateReady, stateDone, Complete)
}

func TestMoveRefusesThirdStateDuplicate(t *testing.T) {
	root := t.TempDir()
	plan := validPlan()
	if err := Draft(root, plan); err != nil {
		t.Fatalf("Draft: %v", err)
	}
	sourceData, err := os.ReadFile(planFile(root, stateDraft, plan.PlanID))
	if err != nil {
		t.Fatalf("ReadFile draft: %v", err)
	}
	if err := os.MkdirAll(planStateDir(root, stateDone), 0o775); err != nil {
		t.Fatalf("MkdirAll done: %v", err)
	}
	if err := os.WriteFile(planFile(root, stateDone, plan.PlanID), sourceData, 0o664); err != nil {
		t.Fatalf("WriteFile done: %v", err)
	}

	err = Approve(root, plan.PlanID)
	want := fmt.Sprintf(
		"backupplan: plan %s exists in both draft and done",
		plan.PlanID,
	)
	assertExactError(t, err, want)
	assertFileBytes(t, planFile(root, stateDraft, plan.PlanID), sourceData)
	assertFileBytes(t, planFile(root, stateDone, plan.PlanID), sourceData)
	assertNotExist(t, planFile(root, stateReady, plan.PlanID))
}

func TestPlanListsMissingDirectoriesAsEmpty(t *testing.T) {
	root := t.TempDir()
	assertPlanLists(t, root, 0, 0, 0)

	drafts, err := ListDraft(root)
	if err != nil {
		t.Fatalf("ListDraft: %v", err)
	}
	if drafts == nil {
		t.Fatal("ListDraft = nil, want empty non-nil slice")
	}
}

func TestPlanPathsRejectInvalidIDsBeforeIO(t *testing.T) {
	root := t.TempDir()
	hostile := "../escape"
	for name, read := range map[string]func(string, string) (Plan, error){
		"ReadDraft": ReadDraft,
		"ReadReady": ReadReady,
		"ReadDone":  ReadDone,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := read(root, hostile)
			want := `backupplan: planID "../escape" must be a 26-character uppercase ` +
				"Crockford base32 ULID"
			assertExactError(t, err, want)
		})
	}
	for name, move := range map[string]func(string, string) error{
		"Approve":  Approve,
		"Complete": Complete,
	} {
		t.Run(name, func(t *testing.T) {
			err := move(root, hostile)
			want := `backupplan: planID "../escape" must be a 26-character uppercase ` +
				"Crockford base32 ULID"
			assertExactError(t, err, want)
		})
	}
}

func assertPlanEncodeAndDecodeRejection(
	t *testing.T,
	mutatePlan func(*Plan),
	mutateWire func(*planWire),
	want string,
) {
	t.Helper()
	plan := validPlan()
	mutatePlan(&plan)
	err := Draft(t.TempDir(), plan)
	assertExactError(t, err, want)

	wire := toWire(validPlan())
	mutateWire(&wire)
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	_, err = decodePlan(data, wire.PlanID)
	assertExactError(t, err, want)
}

func testMoveRefusesOccupiedDestination(
	t *testing.T,
	sourceState, destinationState planState,
	move func(string, string) error,
) {
	t.Helper()
	root := t.TempDir()
	plan := validPlan()
	if err := Draft(root, plan); err != nil {
		t.Fatalf("Draft: %v", err)
	}
	if sourceState == stateReady {
		if err := Approve(root, plan.PlanID); err != nil {
			t.Fatalf("Approve: %v", err)
		}
	}
	sourcePath := planFile(root, sourceState, plan.PlanID)
	sourceData, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("ReadFile source: %v", err)
	}
	destinationPath := planFile(root, destinationState, plan.PlanID)
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o775); err != nil {
		t.Fatalf("MkdirAll destination: %v", err)
	}
	staleData := []byte(`{"stale":"destination must survive"}` + "\n")
	if err := os.WriteFile(destinationPath, staleData, 0o664); err != nil {
		t.Fatalf("WriteFile destination: %v", err)
	}

	err = move(root, plan.PlanID)
	if err == nil {
		replacedData, readErr := os.ReadFile(destinationPath)
		if readErr != nil {
			t.Fatalf("move succeeded; ReadFile replaced destination: %v", readErr)
		}
		t.Fatalf(
			"move succeeded and destination became %q, want rejection preserving %q",
			replacedData,
			staleData,
		)
	}
	action := "approve"
	if sourceState == stateReady {
		action = "complete"
	}
	want := fmt.Sprintf(
		"backupplan: %s %s: cannot move from %q to %q: %s plan already exists",
		action,
		plan.PlanID,
		sourcePath,
		destinationPath,
		destinationState,
	)
	assertExactError(t, err, want)
	assertFileBytes(t, sourcePath, sourceData)
	assertFileBytes(t, destinationPath, staleData)
}

func assertPlanLists(t *testing.T, root string, drafts, ready, done int) {
	t.Helper()
	cases := []struct {
		name string
		list func(string) ([]Plan, error)
		want int
	}{
		{name: "draft", list: ListDraft, want: drafts},
		{name: "ready", list: ListReady, want: ready},
		{name: "done", list: ListDone, want: done},
	}
	for _, tc := range cases {
		got, err := tc.list(root)
		if err != nil {
			t.Fatalf("List%s: %v", tc.name, err)
		}
		if len(got) != tc.want {
			t.Fatalf("List%s count = %d, want %d", tc.name, len(got), tc.want)
		}
	}
}

func assertPlanOnlyAt(t *testing.T, root, planID string, want planState) {
	t.Helper()
	for _, state := range []planState{stateDraft, stateReady, stateDone} {
		_, err := os.Stat(planFile(root, state, planID))
		if state == want {
			if err != nil {
				t.Fatalf("%s plan missing: %v", state, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s plan exists or stat failed: %v", state, err)
		}
	}
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %q: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("ReadFile %q = %q, want %q", path, got, want)
	}
}

func assertNotExist(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stat %q error = %v, want os.ErrNotExist", path, err)
	}
}

func assertExactError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func validPlan() Plan {
	return Plan{
		PlanID:    testPlanID,
		Type:      PlanTypeBackup,
		CreatedAt: time.Date(2026, 7, 27, 9, 14, 22, 0, time.UTC),
		CreatedBy: Creator{
			Version: "v0.6.0",
			Host:    "kura-control-0",
		},
		Target: Target{
			VolumeID:          volume.ID(testVolumeID),
			TapeID:            tape.ID("ABC123L6"),
			RequiredFreeBytes: 30,
			ExpectedSnapshots: []string{testSnapshot, secondSnapshot},
		},
		Items: []Item{
			{
				MetadataRef:        "tvdb:370070",
				Title:              "Show",
				Generation:         8,
				PayloadFingerprint: testFingerprint,
				Bytes:              10,
			},
			{
				MetadataRef:        "anidb:42",
				Title:              "Second Show",
				Generation:         3,
				PayloadFingerprint: "sha256:" + strings.Repeat("b", 64),
				Bytes:              20,
			},
		},
	}
}
