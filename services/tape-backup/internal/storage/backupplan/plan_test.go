package backupplan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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

func TestActionTypesClosedEnum(t *testing.T) {
	for _, action := range validActions() {
		t.Run(string(action.Type), func(t *testing.T) {
			plan := validPlan()
			plan.Actions = []Action{action}
			if err := Draft(t.TempDir(), plan); err != nil {
				t.Fatalf("Draft: %v", err)
			}
		})
	}
}

func TestActionTypeRejectsAbsentAndNull(t *testing.T) {
	action := validActions()[0]
	for _, variant := range []string{"absent", "null"} {
		t.Run(variant, func(t *testing.T) {
			fields := actionJSONFields(t, action)
			if variant == "absent" {
				delete(fields, "type")
			} else {
				fields["type"] = json.RawMessage("null")
			}
			_, err := decodePlanWithActionFields(t, fields)
			assertExactError(t, err, "backupplan: action type is required")
		})
	}
}

func TestPlanMetadataValidationOnEncodeAndDecode(t *testing.T) {
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
			name: "actions required",
			mutatePlan: func(plan *Plan) {
				plan.Actions = nil
			},
			mutateWire: func(wire *planWire) {
				wire.Actions = nil
			},
			want: "backupplan: actions must contain at least one action",
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

func TestBackupActionValidationOnEncodeAndDecode(t *testing.T) {
	cases := []struct {
		name       string
		mutate     func(*Action)
		mutateWire func(*actionWire)
		want       string
	}{
		{
			name: "metadataRef required",
			mutate: func(action *Action) {
				action.MetadataRef = ""
			},
			mutateWire: func(wire *actionWire) {
				wire.MetadataRef = pointer("")
			},
			want: `backupplan: backup action ("", 8): metadataRef is required`,
		},
		{
			name: "generation positive",
			mutate: func(action *Action) {
				action.Generation = 0
			},
			mutateWire: func(wire *actionWire) {
				wire.Generation = pointer(0)
			},
			want: `backupplan: backup action ("tvdb:370070", 0): generation must be at least 1`,
		},
		{
			name: "rootPath required",
			mutate: func(action *Action) {
				action.RootPath = ""
			},
			mutateWire: func(wire *actionWire) {
				wire.RootPath = pointer("")
			},
			want: "backupplan: rootPath is required",
		},
		{
			name: "rootPath traversal",
			mutate: func(action *Action) {
				action.RootPath = "../other-series"
			},
			mutateWire: func(wire *actionWire) {
				wire.RootPath = pointer("../other-series")
			},
			want: `backupplan: rootPath "../other-series" must not contain ..`,
		},
		{
			name: "rootPath absolute",
			mutate: func(action *Action) {
				action.RootPath = "/etc/passwd"
			},
			mutateWire: func(wire *actionWire) {
				wire.RootPath = pointer("/etc/passwd")
			},
			want: `backupplan: rootPath "/etc/passwd" must be relative`,
		},
		{
			name: "rootPath root",
			mutate: func(action *Action) {
				action.RootPath = "/"
			},
			mutateWire: func(wire *actionWire) {
				wire.RootPath = pointer("/")
			},
			want: `backupplan: rootPath "/" must be relative`,
		},
		{
			name: "rootPath dot",
			mutate: func(action *Action) {
				action.RootPath = "."
			},
			mutateWire: func(wire *actionWire) {
				wire.RootPath = pointer(".")
			},
			want: `backupplan: rootPath "." is not canonical`,
		},
		{
			name: "rootPath dot prefix",
			mutate: func(action *Action) {
				action.RootPath = "./Show"
			},
			mutateWire: func(wire *actionWire) {
				wire.RootPath = pointer("./Show")
			},
			want: `backupplan: rootPath "./Show" is not canonical`,
		},
		{
			name: "rootPath trailing slash",
			mutate: func(action *Action) {
				action.RootPath = "Show/"
			},
			mutateWire: func(wire *actionWire) {
				wire.RootPath = pointer("Show/")
			},
			want: `backupplan: rootPath "Show/" is not canonical`,
		},
		{
			name: "rootPath repeated slash",
			mutate: func(action *Action) {
				action.RootPath = "Anime//Show"
			},
			mutateWire: func(wire *actionWire) {
				wire.RootPath = pointer("Anime//Show")
			},
			want: `backupplan: rootPath "Anime//Show" is not canonical`,
		},
		{
			name: "rootPath dot suffix",
			mutate: func(action *Action) {
				action.RootPath = "Show/."
			},
			mutateWire: func(wire *actionWire) {
				wire.RootPath = pointer("Show/.")
			},
			want: `backupplan: rootPath "Show/." is not canonical`,
		},
		{
			name: "rootPath NFC",
			mutate: func(action *Action) {
				action.RootPath = "Cafe\u0301"
			},
			mutateWire: func(wire *actionWire) {
				wire.RootPath = pointer("Cafe\u0301")
			},
			want: `backupplan: rootPath "Café" must be NFC-normalized`,
		},
		{
			name: "rootPath NUL",
			mutate: func(action *Action) {
				action.RootPath = "Show\x00file"
			},
			mutateWire: func(wire *actionWire) {
				wire.RootPath = pointer("Show\x00file")
			},
			want: `backupplan: rootPath "Show\x00file" must not contain NUL or newline`,
		},
		{
			name: "rootPath newline",
			mutate: func(action *Action) {
				action.RootPath = "Show\nfile"
			},
			mutateWire: func(wire *actionWire) {
				wire.RootPath = pointer("Show\nfile")
			},
			want: `backupplan: rootPath "Show\nfile" must not contain NUL or newline`,
		},
		{
			name: "fingerprint format",
			mutate: func(action *Action) {
				action.PayloadFingerprint = "bad"
			},
			mutateWire: func(wire *actionWire) {
				wire.PayloadFingerprint = pointer("bad")
			},
			want: "backupplan: payloadFingerprint must use algorithm:digest format",
		},
		{
			name: "bytes negative",
			mutate: func(action *Action) {
				action.Bytes = -1
			},
			mutateWire: func(wire *actionWire) {
				wire.Bytes = pointer(int64(-1))
			},
			want: `backupplan: bytes for backup action ("tvdb:370070", 8) must not be negative`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertActionEncodeAndDecodeRejection(t, tc.mutate, tc.mutateWire, tc.want)
		})
	}
}

func TestDuplicateBackupActionRejectedAtPlanScope(t *testing.T) {
	plan := validPlan()
	plan.Actions = append(plan.Actions, plan.Actions[0])
	err := Draft(t.TempDir(), plan)
	const want = `backupplan: duplicate backup action ("tvdb:370070", 8)`
	assertExactError(t, err, want)

	wire := toWire(validPlan())
	wire.Actions = append(wire.Actions, wire.Actions[0])
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	_, err = decodePlan(data, testPlanID)
	assertExactError(t, err, want)
}

func TestPlanAllowsExecutorOwnedInventoryAndCapacityChecks(t *testing.T) {
	plan := validPlan()
	plan.Actions = []Action{
		plan.Actions[0],
		{
			Type:      ActionAssertInventory,
			Snapshots: []string{"OR3GIYR2GM3TAMBXGA.g8"},
		},
		{
			Type:  ActionAssertFreeSpace,
			Bytes: 999,
		},
	}
	if err := Draft(t.TempDir(), plan); err != nil {
		t.Fatalf("Draft: %v", err)
	}
}

func TestActionValidationOnEncodeAndDecode(t *testing.T) {
	cases := []struct {
		name   string
		action Action
		want   string
	}{
		{
			name:   "unknown type",
			action: Action{Type: "unknown"},
			want:   `backupplan: unsupported action type "unknown"`,
		},
		{
			name:   "assert volume ID",
			action: Action{Type: ActionAssertVolume, VolumeID: "bad"},
			want: `backupplan: volumeID "bad" must be a 26-character uppercase ` +
				"Crockford base32 ULID",
		},
		{
			name: "assert inventory canonical snapshot",
			action: Action{
				Type:      ActionAssertInventory,
				Snapshots: []string{strings.ToLower(testSnapshot)},
			},
			want: `backupplan: snapshot "or3giyr2gm3tambxga.g7": snapshot name ` +
				`"or3giyr2gm3tambxga.g7" has invalid base32 metadataRef: ` +
				"illegal base32 data at input byte 0",
		},
		{
			name: "assert inventory duplicate",
			action: Action{
				Type:      ActionAssertInventory,
				Snapshots: []string{testSnapshot, testSnapshot},
			},
			want: `backupplan: duplicate snapshot "OR3GIYR2GM3TAMBXGA.g7"`,
		},
		{
			name:   "assert free space negative",
			action: Action{Type: ActionAssertFreeSpace, Bytes: -1},
			want:   "backupplan: assert_free_space bytes must not be negative",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := validPlan()
			plan.Actions = []Action{tc.action}
			err := Draft(t.TempDir(), plan)
			assertExactError(t, err, tc.want)

			wire := toWire(plan)
			data, err := json.Marshal(wire)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			_, err = decodePlan(data, testPlanID)
			assertExactError(t, err, tc.want)
		})
	}
}

func TestActionRequiredFieldsRejectAbsentAndNull(t *testing.T) {
	for _, action := range validActions() {
		_, required, err := actionWireRule(action.Type)
		if err != nil {
			t.Fatalf("actionWireRule(%s): %v", action.Type, err)
		}
		for _, field := range actionWireFields() {
			if required&field.mask == 0 {
				continue
			}
			for _, variant := range []string{"absent", "null"} {
				t.Run(string(action.Type)+"/"+field.name+"/"+variant, func(t *testing.T) {
					fields := actionJSONFields(t, action)
					if variant == "absent" {
						delete(fields, field.name)
					} else {
						fields[field.name] = json.RawMessage("null")
					}
					_, err := decodePlanWithActionFields(t, fields)
					want := fmt.Sprintf(
						"backupplan: %s action requires %s",
						action.Type,
						field.name,
					)
					assertExactError(t, err, want)
				})
			}
		}
	}
}

func TestPlanDecodeRejectsDuplicateCaseFoldedFields(t *testing.T) {
	firstKeyByCase := map[string]string{
		"nested object in array": "Bytes",
		"byte-identical keys":    "bytes",
	}
	data, err := json.Marshal(toWire(validPlan()))
	if err != nil {
		t.Fatalf("Marshal plan: %v", err)
	}
	for name, first := range firstKeyByCase {
		t.Run(name, func(t *testing.T) {
			newFragment := fmt.Sprintf(`"bytes":10,%q:null`, first)
			mutated := bytes.Replace(
				data,
				[]byte(`"bytes":10`),
				[]byte(newFragment),
				1,
			)
			if bytes.Equal(mutated, data) {
				t.Fatal(`plan does not contain "bytes":10`)
			}
			_, err := decodePlan(mutated, testPlanID)
			want := fmt.Sprintf(
				"backupplan: decode plan %s: duplicate case-folded JSON fields "+
					"at actions[0].bytes: %q and %q",
				testPlanID,
				first,
				"bytes",
			)
			assertExactError(t, err, want)
		})
	}
}

// The reported path must name the action that actually holds the duplicate.
// Every other case injects into the first action, where a dropped or zeroed
// array index still reads correctly.
func TestPlanDuplicateFieldPathNamesTheRightAction(t *testing.T) {
	data, err := json.Marshal(toWire(validPlan()))
	if err != nil {
		t.Fatalf("Marshal plan: %v", err)
	}
	// "bytes":30 belongs to the fifth action, the trailing assert_free_space.
	mutated := bytes.Replace(
		data,
		[]byte(`"bytes":30`),
		[]byte(`"bytes":30,"Bytes":null`),
		1,
	)
	if bytes.Equal(mutated, data) {
		t.Fatal(`plan does not contain "bytes":30`)
	}
	_, err = decodePlan(mutated, testPlanID)
	want := fmt.Sprintf(
		"backupplan: decode plan %s: duplicate case-folded JSON fields "+
			"at actions[4].bytes: %q and %q",
		testPlanID,
		"Bytes",
		"bytes",
	)
	assertExactError(t, err, want)
}

func TestActionMixedCaseForbiddenFieldsRejected(t *testing.T) {
	tested := 0
	for _, action := range validActions() {
		allowed, _, err := actionWireRule(action.Type)
		if err != nil {
			t.Fatalf("actionWireRule(%s): %v", action.Type, err)
		}
		for _, field := range actionTestFields() {
			if allowed&field.mask != 0 {
				continue
			}
			tested++
			t.Run(string(action.Type)+"/"+field.name, func(t *testing.T) {
				fields := actionJSONFields(t, action)
				mixedCaseName := strings.ToUpper(field.name[:1]) + field.name[1:]
				fields[mixedCaseName] = field.zero

				_, err := decodePlanWithActionFields(t, fields)
				want := fmt.Sprintf(
					"backupplan: %s action must not contain %s",
					action.Type,
					field.name,
				)
				assertExactError(t, err, want)
			})
		}
	}
	const wantCases = 47
	if tested != wantCases {
		t.Fatalf("mixed-case forbidden action field cases = %d, want %d", tested, wantCases)
	}
}

func TestActionForbiddenFieldsRejectNonzeroZeroAndNull(t *testing.T) {
	fields := actionTestFields()
	tested := 0
	for _, action := range validActions() {
		allowed, _, err := actionWireRule(action.Type)
		if err != nil {
			t.Fatalf("actionWireRule(%s): %v", action.Type, err)
		}
		for _, field := range fields {
			if allowed&field.mask != 0 {
				continue
			}
			tested++
			t.Run(string(action.Type)+"/"+field.name, func(t *testing.T) {
				invalid := action
				field.set(&invalid)
				plan := validPlan()
				plan.Actions = []Action{invalid}
				err := Draft(t.TempDir(), plan)
				want := fmt.Sprintf(
					"backupplan: %s action must not contain %s",
					action.Type,
					field.name,
				)
				assertExactError(t, err, want)

				for _, raw := range []json.RawMessage{field.zero, json.RawMessage("null")} {
					wireFields := actionJSONFields(t, action)
					wireFields[field.name] = raw
					_, err = decodePlanWithActionFields(t, wireFields)
					assertExactError(t, err, want)
				}
			})
		}
	}
	const wantCases = 47
	if tested != wantCases {
		t.Fatalf("forbidden action field cases = %d, want %d", tested, wantCases)
	}
}

func TestAssertInventorySnapshotsNilAndEmptyDistinction(t *testing.T) {
	t.Run("nil_is_missing", func(t *testing.T) {
		plan := validPlan()
		plan.Actions = []Action{{
			Type:      ActionAssertInventory,
			Snapshots: nil,
		}}
		err := Draft(t.TempDir(), plan)
		assertExactError(t, err, "backupplan: snapshots is required")
	})

	t.Run("empty_asserts_blank_cartridge_and_round_trips", func(t *testing.T) {
		root := t.TempDir()
		plan := validPlan()
		plan.Actions = []Action{{
			Type:      ActionAssertInventory,
			Snapshots: []string{},
		}}
		if err := Draft(root, plan); err != nil {
			t.Fatalf("Draft: %v", err)
		}
		got, err := ReadDraft(root, plan.PlanID)
		if err != nil {
			t.Fatalf("ReadDraft: %v", err)
		}
		if !reflect.DeepEqual(got.Actions, plan.Actions) {
			t.Fatalf("ReadDraft Actions = %#v, want %#v", got.Actions, plan.Actions)
		}
	})
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
			name: "metadataRef valid UTF-8",
			mutate: func(plan *Plan) {
				plan.Actions[0].MetadataRef = "tvdb:\xff"
			},
			want: `backupplan: backup action ("tvdb:\xff", 8): metadataRef must be valid UTF-8`,
		},
		{
			name: "rootPath valid UTF-8",
			mutate: func(plan *Plan) {
				plan.Actions[0].RootPath = "Sh\xffw"
			},
			want: `backupplan: rootPath "Sh\xffw" is not valid UTF-8`,
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
		{name: "rootPath", old: "Show", new: []byte("Sh\xffw")},
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

func TestOneLivePlanPerTape(t *testing.T) {
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
				"backupplan: tape %s already has live plan %s",
				"ABC123L6",
				testPlanID,
			)
			assertExactError(t, err, want)
			assertNotExist(t, planFile(root, stateDraft, secondPlanID))
		})
	}
}

func TestCompletedPlanDoesNotBlockNewPlanForTape(t *testing.T) {
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

func assertActionEncodeAndDecodeRejection(
	t *testing.T,
	mutateAction func(*Action),
	mutateWire func(*actionWire),
	want string,
) {
	t.Helper()
	plan := validPlan()
	mutateAction(&plan.Actions[0])
	err := Draft(t.TempDir(), plan)
	assertExactError(t, err, want)

	wire := toWire(validPlan())
	mutateWire(&wire.Actions[0])
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	_, err = decodePlan(data, wire.PlanID)
	assertExactError(t, err, want)
}

type actionTestField struct {
	name string
	mask actionFields
	zero json.RawMessage
	set  func(*Action)
}

func actionTestFields() []actionTestField {
	return []actionTestField{
		{
			name: "metadataRef", mask: actionFieldMetadataRef,
			zero: json.RawMessage(`""`),
			set:  func(action *Action) { action.MetadataRef = "tvdb:370070" },
		},
		{
			name: "rootPath", mask: actionFieldRootPath,
			zero: json.RawMessage(`""`),
			set:  func(action *Action) { action.RootPath = "Show" },
		},
		{
			name: "generation", mask: actionFieldGeneration,
			zero: json.RawMessage(`0`),
			set:  func(action *Action) { action.Generation = 8 },
		},
		{
			name: "payloadFingerprint", mask: actionFieldPayloadFingerprint,
			zero: json.RawMessage(`""`),
			set:  func(action *Action) { action.PayloadFingerprint = testFingerprint },
		},
		{
			name: "bytes", mask: actionFieldBytes,
			zero: json.RawMessage(`0`),
			set:  func(action *Action) { action.Bytes = 10 },
		},
		{
			name: "volumeID", mask: actionFieldVolumeID,
			zero: json.RawMessage(`""`),
			set:  func(action *Action) { action.VolumeID = volume.ID(testVolumeID) },
		},
		{
			name: "snapshots", mask: actionFieldSnapshots,
			zero: json.RawMessage(`[]`),
			set:  func(action *Action) { action.Snapshots = []string{testSnapshot} },
		},
	}
}

func actionJSONFields(t *testing.T, action Action) map[string]json.RawMessage {
	t.Helper()
	data, err := json.Marshal(toActionWire(action))
	if err != nil {
		t.Fatalf("Marshal action: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("Unmarshal action fields: %v", err)
	}
	return fields
}

func decodePlanWithActionFields(
	t *testing.T,
	actionFields map[string]json.RawMessage,
) (Plan, error) {
	t.Helper()
	data, err := json.Marshal(toWire(validPlan()))
	if err != nil {
		t.Fatalf("Marshal plan: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("Unmarshal plan fields: %v", err)
	}
	actionData, err := json.Marshal(actionFields)
	if err != nil {
		t.Fatalf("Marshal action fields: %v", err)
	}
	return decodePlanWithActionJSON(t, actionData)
}

func decodePlanWithActionJSON(t *testing.T, actionData []byte) (Plan, error) {
	t.Helper()
	data, err := json.Marshal(toWire(validPlan()))
	if err != nil {
		t.Fatalf("Marshal plan: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("Unmarshal plan fields: %v", err)
	}
	fields["actions"] = json.RawMessage("[" + string(actionData) + "]")
	data, err = json.Marshal(fields)
	if err != nil {
		t.Fatalf("Marshal mutated plan: %v", err)
	}
	return decodePlan(data, testPlanID)
}

func jsonObjectWithDuplicateFoldedField(
	t *testing.T,
	fields map[string]json.RawMessage,
	name string,
	value json.RawMessage,
	nullFirst bool,
) []byte {
	t.Helper()
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("Marshal fields: %v", err)
	}
	mixedCaseName := strings.ToUpper(name[:1]) + name[1:]
	firstName, firstValue := name, value
	secondName, secondValue := mixedCaseName, json.RawMessage("null")
	if nullFirst {
		firstName, secondName = secondName, firstName
		firstValue, secondValue = secondValue, firstValue
	}
	duplicate := fmt.Sprintf(
		`%q:%s,%q:%s`,
		firstName,
		firstValue,
		secondName,
		secondValue,
	)
	if len(fields) == 0 {
		return []byte("{" + duplicate + "}")
	}
	return append(data[:len(data)-1], []byte(","+duplicate+"}")...)
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
		CreatedAt: time.Date(2026, 7, 27, 9, 14, 22, 0, time.UTC),
		CreatedBy: Creator{
			Version: "v0.6.0",
			Host:    "kura-control-0",
		},
		Target: Target{
			TapeID: tape.ID("ABC123L6"),
		},
		Actions: []Action{
			{
				Type:               ActionBackup,
				MetadataRef:        "tvdb:370070",
				RootPath:           "Show",
				Generation:         8,
				PayloadFingerprint: testFingerprint,
				Bytes:              10,
			},
			{
				Type:               ActionBackup,
				MetadataRef:        "anidb:42",
				RootPath:           "Second Show",
				Generation:         3,
				PayloadFingerprint: "sha256:" + strings.Repeat("b", 64),
				Bytes:              20,
			},
			{
				Type:     ActionAssertVolume,
				VolumeID: volume.ID(testVolumeID),
			},
			{
				Type:      ActionAssertInventory,
				Snapshots: []string{testSnapshot, secondSnapshot},
			},
			{
				Type:  ActionAssertFreeSpace,
				Bytes: 30,
			},
		},
	}
}

func validActions() []Action {
	return []Action{
		{
			Type:               ActionBackup,
			MetadataRef:        "tvdb:370070",
			RootPath:           "Show",
			Generation:         8,
			PayloadFingerprint: testFingerprint,
			Bytes:              10,
		},
		{Type: ActionAssertVolume, VolumeID: volume.ID(testVolumeID)},
		{Type: ActionAssertInventory, Snapshots: []string{}},
		{Type: ActionAssertFreeSpace, Bytes: 0},
		{Type: ActionAdmit, VolumeID: volume.ID(testVolumeID)},
		{Type: ActionReformat},
		{Type: ActionImport},
		{Type: ActionVerify},
	}
}
