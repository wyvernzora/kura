package tape_test

import (
	"testing"

	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
)

func TestTapeIDValidation(t *testing.T) {
	cases := []struct {
		name string
		id   tape.ID
		want string
	}{
		{name: "LTO-1 data", id: "ABC123L1"},
		{name: "LTO-2 data", id: "ABC123L2"},
		{name: "LTO-3 data", id: "ABC123L3"},
		{name: "LTO-4 data", id: "ABC123L4"},
		{name: "LTO-5 data", id: "ABC123L5"},
		{name: "LTO-6 data", id: "ABC123L6"},
		{name: "LTO-7 data", id: "ABC123L7"},
		{name: "LTO-8 data", id: "ABC123L8"},
		{name: "LTO-9 data", id: "ABC123L9"},
		{name: "LTO-10 data", id: "ABC123LA"},
		{name: "LTO-7 Type M", id: "ABC123M8"},
		{
			name: "empty ID rejected",
			id:   "",
			want: "tapeID is required",
		},
		{
			name: "cleaning cartridge rejected",
			id:   "ABC123CU",
			want: `tapeID "ABC123CU" identifies a cleaning cartridge`,
		},
		{
			name: "WORM cartridge rejected",
			id:   "ABC123LW",
			want: `tapeID "ABC123LW" identifies WORM media`,
		},
		{
			name: "unassigned LR rejected as unsupported",
			id:   "ABC123LR",
			want: `tapeID "ABC123LR" has unsupported media identifier "LR"`,
		},
		{
			name: "unassigned LS rejected as unsupported",
			id:   "ABC123LS",
			want: `tapeID "ABC123LS" has unsupported media identifier "LS"`,
		},
		{
			name: "unsupported media identifier rejected",
			id:   "ABC123ZZ",
			want: `tapeID "ABC123ZZ" has unsupported media identifier "ZZ"`,
		},
		{
			name: "lowercase serial rejected",
			id:   "abc123L6",
			want: `tapeID "abc123L6" must use uppercase letters`,
		},
		{
			name: "seven characters rejected",
			id:   "ABC12L6",
			want: `tapeID "ABC12L6" must be exactly 8 characters`,
		},
		{
			name: "nine characters rejected",
			id:   "ABCD123L6",
			want: `tapeID "ABCD123L6" must be exactly 8 characters`,
		},
		{
			name: "lowercase suffix rejected",
			id:   "ABC123l6",
			want: `tapeID "ABC123l6" must use uppercase letters`,
		},
		{
			name: "non-alphanumeric serial rejected",
			id:   "ABC-23L6",
			want: `tapeID "ABC-23L6" volume serial must contain only A-Z and 0-9`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tape.ParseID(string(tc.id))
			if tc.want == "" {
				if err != nil {
					t.Fatalf("ParseID: %v", err)
				}
				if got != tc.id {
					t.Fatalf("ParseID = %q, want %q", got, tc.id)
				}
				return
			}
			if err == nil || err.Error() != tc.want {
				t.Fatalf("ParseID error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestTapeIDMediaGeneration(t *testing.T) {
	cases := []struct {
		name string
		id   tape.ID
		want string
	}{
		{name: "LTO-1", id: "ABC123L1", want: "LTO-1"},
		{name: "LTO-2", id: "ABC123L2", want: "LTO-2"},
		{name: "LTO-3", id: "ABC123L3", want: "LTO-3"},
		{name: "LTO-4", id: "ABC123L4", want: "LTO-4"},
		{name: "LTO-5", id: "ABC123L5", want: "LTO-5"},
		{name: "LTO-6", id: "ABC123L6", want: "LTO-6"},
		{name: "LTO-7", id: "ABC123L7", want: "LTO-7"},
		{name: "LTO-8", id: "ABC123L8", want: "LTO-8"},
		{name: "LTO-9", id: "ABC123L9", want: "LTO-9"},
		{name: "LTO-10", id: "ABC123LA", want: "LTO-10"},
		{name: "LTO-7 Type M", id: "ABC123M8", want: "LTO-7 Type M"},
		{name: "L0", id: "ABC123L0"},
		{name: "unsupported identifier", id: "ABC123ZZ"},
		{name: "cleaning cartridge", id: "ABC123CU"},
		{name: "WORM cartridge", id: "ABC123LW"},
		{name: "lowercase", id: "abc123l6"},
		{name: "invalid serial with allowlisted suffix", id: "ABC-23L6"},
		{name: "short", id: "ABC"},
		{name: "empty", id: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.id.MediaGeneration(); got != tc.want {
				t.Fatalf("MediaGeneration = %q, want %q", got, tc.want)
			}
		})
	}
}
