package main

import (
	"bytes"
	"slices"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/response"
)

func TestTagFlagsAreRepeatable(t *testing.T) {
	var flags cli
	parser, err := kong.New(&flags)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	if _, err := parser.Parse([]string{
		"tag", "update", "tvdb:42",
		"--tag", "test",
		"--tag", "!foo",
	}); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if want := []string{"test", "!foo"}; !slices.Equal(flags.Tag.Update.Tags, want) {
		t.Fatalf("tag update flags = %#v, want %#v", flags.Tag.Update.Tags, want)
	}
}

func TestListTagFlagsAreRepeatable(t *testing.T) {
	var flags cli
	parser, err := kong.New(&flags)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	if _, err := parser.Parse([]string{
		"list",
		"--tag", "priority",
		"--tag", "!mute-notifications",
	}); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if want := []string{"priority", "!mute-notifications"}; !slices.Equal(flags.List.Tags, want) {
		t.Fatalf("list tag flags = %#v, want %#v", flags.List.Tags, want)
	}
}

func TestPrintTags(t *testing.T) {
	var out bytes.Buffer
	err := printTags(&out, response.SeriesTags{
		MetadataRef: refs.Metadata("tvdb:42"),
		Tags:        []string{"maintenance-requested", "priority"},
	}, false)
	if err != nil {
		t.Fatalf("printTags: %v", err)
	}
	if got := out.String(); got != "maintenance-requested\npriority\n" {
		t.Fatalf("output = %q", got)
	}
}

func TestPrintTagsAndAliasesEmptyMessages(t *testing.T) {
	var out bytes.Buffer
	if err := printTags(&out, response.SeriesTags{Tags: []string{}}, false); err != nil {
		t.Fatalf("printTags: %v", err)
	}
	if got := out.String(); got != "(no tags)\n" {
		t.Fatalf("tag output = %q", got)
	}

	out.Reset()
	if err := printAliases(&out, response.UserAliasList{Aliases: []string{}}, false); err != nil {
		t.Fatalf("printAliases: %v", err)
	}
	if got := out.String(); got != "(no user aliases)\n" {
		t.Fatalf("alias output = %q", got)
	}
}
