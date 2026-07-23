package media_test

import (
	"strings"
	"testing"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/media"
)

func TestValidateAttrs(t *testing.T) {
	tests := []struct {
		name  string
		attrs media.Attrs
		ok    bool
	}{
		{name: "empty", attrs: nil, ok: true},
		{name: "valid", attrs: media.Attrs{"origin": "takuhai", "takuhai.infohash": "abc"}, ok: true},
		{name: "too many", attrs: func() media.Attrs {
			out := media.Attrs{}
			for i := range media.MaxAttrs + 1 {
				out["k"+string(rune('a'+i))] = "v"
			}
			return out
		}()},
		{name: "bad key", attrs: media.Attrs{"ReleaseGroup": "SubsPlease"}},
		{name: "long key", attrs: media.Attrs{strings.Repeat("a", media.MaxAttrKeyLen+1): "v"}},
		{name: "multibyte value max chars", attrs: media.Attrs{"origin_title": strings.Repeat("あ", media.MaxAttrValLen)}, ok: true},
		{name: "long value", attrs: media.Attrs{"origin_title": strings.Repeat("a", media.MaxAttrValLen+1)}},
		{name: "long multibyte value", attrs: media.Attrs{"origin_title": strings.Repeat("あ", media.MaxAttrValLen+1)}},
		{name: "control char", attrs: media.Attrs{"origin_title": "line\nbreak"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := media.ValidateAttrs(tt.attrs)
			if tt.ok && err != nil {
				t.Fatalf("ValidateAttrs() = %v, want nil", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("ValidateAttrs() = nil, want error")
			}
		})
	}
}

func TestCloneRecordCopiesAttrs(t *testing.T) {
	original := media.Record{Attrs: media.Attrs{"origin": "takuhai"}}
	cloned := media.CloneRecord(original)
	cloned.Attrs["origin"] = "other"
	if original.Attrs["origin"] != "takuhai" {
		t.Fatalf("original attr mutated to %q", original.Attrs["origin"])
	}
}
