package main

import "testing"

func TestParseStageAttrs(t *testing.T) {
	attrs, err := parseStageAttrs([]string{"origin=takuhai", "release_group=SubsPlease", "empty="})
	if err != nil {
		t.Fatalf("parseStageAttrs: %v", err)
	}
	if attrs["origin"] != "takuhai" || attrs["release_group"] != "SubsPlease" || attrs["empty"] != "" {
		t.Fatalf("attrs = %#v", attrs)
	}
}

func TestParseStageAttrsRejectsMalformed(t *testing.T) {
	tests := [][]string{
		{"origin"},
		{"=takuhai"},
		{"origin=one", "origin=two"},
	}
	for _, tc := range tests {
		if _, err := parseStageAttrs(tc); err == nil {
			t.Fatalf("parseStageAttrs(%v) = nil, want error", tc)
		}
	}
}
