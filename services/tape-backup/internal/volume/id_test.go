package volume_test

import (
	"testing"

	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
)

func TestParseID(t *testing.T) {
	const valid = "01J8ZQ7W5TWHA6R6J8X4QZ9Y7V"
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{name: "valid", value: valid},
		{name: "empty", want: "volumeID is required"},
		{
			name:  "short",
			value: "01J8ZQ",
			want:  `volumeID "01J8ZQ" must be a 26-character uppercase Crockford base32 ULID`,
		},
		{
			name:  "lowercase",
			value: "01j8zq7w5twha6r6j8x4qz9y7v",
			want:  `volumeID "01j8zq7w5twha6r6j8x4qz9y7v" must be a 26-character uppercase Crockford base32 ULID`,
		},
		{
			name:  "invalid Crockford character",
			value: "01J8ZQ7W5TWHA6R6J8X4QZ9Y7I",
			want:  `volumeID "01J8ZQ7W5TWHA6R6J8X4QZ9Y7I" must be a 26-character uppercase Crockford base32 ULID`,
		},
		{
			name:  "timestamp overflow",
			value: "81J8ZQ7W5TWHA6R6J8X4QZ9Y7V",
			want:  `volumeID "81J8ZQ7W5TWHA6R6J8X4QZ9Y7V" must be a 26-character uppercase Crockford base32 ULID`,
		},
		{
			name:  "path traversal",
			value: "../01J8ZQ7W5TWHA6R6J8X4Q",
			want:  `volumeID "../01J8ZQ7W5TWHA6R6J8X4Q" must be a 26-character uppercase Crockford base32 ULID`,
		},
		{
			name:  "forward slash",
			value: "01J8ZQ7W5TWHA6R6J8X4QZ/7V",
			want:  `volumeID "01J8ZQ7W5TWHA6R6J8X4QZ/7V" must be a 26-character uppercase Crockford base32 ULID`,
		},
		{
			name:  "backslash",
			value: `01J8ZQ7W5TWHA6R6J8X4QZ\7V`,
			want:  `volumeID "01J8ZQ7W5TWHA6R6J8X4QZ\\7V" must be a 26-character uppercase Crockford base32 ULID`,
		},
		{
			name:  "space",
			value: "01J8ZQ7W5TWHA6R6J8X4QZ 7V",
			want:  `volumeID "01J8ZQ7W5TWHA6R6J8X4QZ 7V" must be a 26-character uppercase Crockford base32 ULID`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := volume.ParseID(tc.value)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("ParseID: %v", err)
				}
				if got != volume.ID(valid) {
					t.Fatalf("ParseID = %q, want %q", got, valid)
				}
				return
			}
			if err == nil || err.Error() != tc.want {
				t.Fatalf("ParseID error = %v, want %q", err, tc.want)
			}
		})
	}
}
