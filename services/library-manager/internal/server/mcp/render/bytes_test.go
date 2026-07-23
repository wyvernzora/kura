package render

import "testing"

func TestHumanByteSize(t *testing.T) {
	gb := int64(1 << 30)
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{1, "1B"},
		{1023, "1023B"},
		{1024, "1.0KB"},
		{1500, "1.5KB"},
		{1 << 20, "1.0MB"},
		{int64(float64(gb) * 1.15), "1.15GB"},
		{1 << 40, "1.00TB"},
		{-1, "-"},
	}
	for _, tc := range cases {
		if got := HumanByteSize(tc.in); got != tc.want {
			t.Errorf("HumanByteSize(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
