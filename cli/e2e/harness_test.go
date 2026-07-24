//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/library-manager/pkg/api"
	"github.com/wyvernzora/kura/services/library-manager/pkg/api/refs"
	"rsc.io/script"
)

// newEngine returns a *script.Engine with DefaultCmds plus kura_*
// commands that subprocess against the supplied daemon. Migrated from
// the in-process workflow.* harness — every kura_X is now an
// invocation of bin/kura-e2e <verb> with KURA_SERVER_URL pointing at
// the daemon.
//
// Commands that are pure script-side fixtures (kura_mkfile,
// kura_write_series_file, kura_series_dir) keep their in-process
// implementations; they don't go through the binary.
func newEngine(t *testing.T, b *e2eBinary) *script.Engine {
	t.Helper()
	cmds := script.DefaultCmds()

	// ── kura_add ──────────────────────────────────────────────────────────
	cmds["kura_add"] = script.Command(
		script.CmdUsage{Summary: "add a series by metadata ref", Args: "<metadata_ref>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("kura_add: expected 1 arg, got %d", len(args))
			}
			out, _, runErr := b.run(s.Context(), "add", "--json", args[0])
			if runErr != nil {
				return nil, fmt.Errorf("kura_add: %w", runErr)
			}
			var result map[string]any
			if err := json.Unmarshal([]byte(out), &result); err != nil {
				return nil, fmt.Errorf("kura_add: decode: %w (out=%s)", err, out)
			}
			// Per Product.md "Selectors, not paths" REST identifies
			// series by metadata ref. Stash it in KURA_LAST_REF so
			// downstream `kura_X $KURA_LAST_REF` invocations pass a
			// metadata ref to the binary, which resolves it through
			// /resolve like a real CLI user would. The series ref
			// (directory name) goes into KURA_LAST_SERIES_REF for
			// the fixture commands that touch on-disk paths.
			metadataRef, _ := result["metadataRef"].(string)
			seriesRef, _ := result["ref"].(string)
			return func(s *script.State) (stdout, stderr string, err error) {
				if metadataRef != "" {
					if setErr := s.Setenv("KURA_LAST_REF", metadataRef); setErr != nil {
						return "", "", fmt.Errorf("kura_add: set env: %w", setErr)
					}
				}
				if seriesRef != "" {
					if setErr := s.Setenv("KURA_LAST_SERIES_REF", seriesRef); setErr != nil {
						return "", "", fmt.Errorf("kura_add: set env: %w", setErr)
					}
				}
				return compactIfJSON(out), "", nil
			}, nil
		},
	)

	// ── kura_show ─────────────────────────────────────────────────────────
	cmds["kura_show"] = script.Command(
		script.CmdUsage{Summary: "show a series by ref", Args: "<series_ref>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) == 0 {
				return nil, fmt.Errorf("kura_show: expected series ref argument")
			}
			ref := s.ExpandEnv(strings.Join(args, " "), false)
			out, _, err := b.run(s.Context(), "show", "--json", ref)
			if err != nil {
				return nil, fmt.Errorf("kura_show: %w", err)
			}
			return staticOutput(out), nil
		},
	)

	// ── kura_tag_update ─────────────────────────────────────────────────
	cmds["kura_tag_update"] = script.Command(
		script.CmdUsage{Summary: "update series tags", Args: "<metadata_ref> <tag_expression...>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) < 2 {
				return nil, fmt.Errorf("kura_tag_update: expected <metadata_ref> <tag_expression...>")
			}
			ref := s.ExpandEnv(args[0], false)
			cliArgs := []string{"tag", "update", "--json"}
			for _, tag := range args[1:] {
				cliArgs = append(cliArgs, "--tag", tag)
			}
			cliArgs = append(cliArgs, ref)
			out, errOut, runErr := b.run(s.Context(), cliArgs...)
			return func(s *script.State) (string, string, error) {
				return compactIfJSON(out), errOut, runErr
			}, nil
		},
	)

	// ── kura_show_ep ──────────────────────────────────────────────────────
	cmds["kura_show_ep"] = script.Command(
		script.CmdUsage{Summary: "show series filtered by episode selector", Args: "<series_ref> <selector>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("kura_show_ep: expected <series_ref> <selector>")
			}
			ref := s.ExpandEnv(args[0], false)
			out, _, err := b.run(s.Context(), "show", "--json", "--episodes", args[1], ref)
			if err != nil {
				return nil, fmt.Errorf("kura_show_ep: %w", err)
			}
			return staticOutput(out), nil
		},
	)

	// ── kura_show_status ──────────────────────────────────────────────────
	cmds["kura_show_status"] = script.Command(
		script.CmdUsage{Summary: "show series filtered by status", Args: "<series_ref> <status>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("kura_show_status: expected <series_ref> <status>")
			}
			ref := s.ExpandEnv(args[0], false)
			out, _, err := b.run(s.Context(), "show", "--json", "--status", args[1], ref)
			if err != nil {
				return nil, fmt.Errorf("kura_show_status: %w", err)
			}
			return staticOutput(out), nil
		},
	)

	// inboxSelector wraps s.Path(rel) → inbox: selector form. The
	// e2e harness pins workdir == inboxRoot, so anything written via
	// `cp` in a scenario is guaranteed to fall under the inbox.
	inboxSelector := func(s *script.State, rel string) (string, error) {
		abs := s.Path(rel)
		relUnderInbox, err := filepath.Rel(b.inboxRoot, abs)
		if err != nil {
			return "", err
		}
		if strings.HasPrefix(relUnderInbox, "..") {
			return "", fmt.Errorf("%q is not under inbox root %q", abs, b.inboxRoot)
		}
		return "inbox:" + filepath.ToSlash(relUnderInbox), nil
	}

	// ── kura_inbox_list ─────────────────────────────────────────────────
	cmds["kura_inbox_list"] = script.Command(
		script.CmdUsage{Summary: "list an inbox directory or exact file", Args: "<path>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("kura_inbox_list: expected 1 arg, got %d", len(args))
			}
			out, errOut, runErr := b.run(
				s.Context(),
				"inbox", "list", "--json", "--depth=3", "--limit=500",
				s.ExpandEnv(args[0], false),
			)
			return func(s *script.State) (string, string, error) {
				return compactIfJSON(out), errOut, runErr
			}, nil
		},
	)

	// ── kura_stage ────────────────────────────────────────────────────────
	cmds["kura_stage"] = script.Command(
		script.CmdUsage{Summary: "stage an episode file", Args: "<series_ref> <episode_marker> <file>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) != 3 {
				return nil, fmt.Errorf("kura_stage: expected 3 args, got %d", len(args))
			}
			ref := s.ExpandEnv(args[0], false)
			selector, err := inboxSelector(s, args[2])
			if err != nil {
				return nil, fmt.Errorf("kura_stage: %w", err)
			}
			out, _, err := b.run(s.Context(), "stage", "episode", "--json", ref, args[1], selector)
			if err != nil {
				return nil, fmt.Errorf("kura_stage: %w", err)
			}
			return staticOutput(out), nil
		},
	)

	// ── kura_stage_replace ────────────────────────────────────────────────
	cmds["kura_stage_replace"] = script.Command(
		script.CmdUsage{Summary: "stage an episode replacement", Args: "<series_ref> <episode_marker> <file>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) != 3 {
				return nil, fmt.Errorf("kura_stage_replace: expected 3 args, got %d", len(args))
			}
			ref := s.ExpandEnv(args[0], false)
			selector, err := inboxSelector(s, args[2])
			if err != nil {
				return nil, fmt.Errorf("kura_stage_replace: %w", err)
			}
			out, _, err := b.run(s.Context(), "stage", "episode", "--json", "--replace", ref, args[1], selector)
			if err != nil {
				return nil, fmt.Errorf("kura_stage_replace: %w", err)
			}
			return staticOutput(out), nil
		},
	)

	// ── kura_stage_attrs ─────────────────────────────────────────────────
	cmds["kura_stage_attrs"] = script.Command(
		script.CmdUsage{Summary: "stage an episode file with attrs", Args: "<series_ref> <episode_marker> <file> <key=value...>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) < 4 {
				return nil, fmt.Errorf("kura_stage_attrs: expected <ref> <ep> <file> <key=value...>")
			}
			ref := s.ExpandEnv(args[0], false)
			selector, err := inboxSelector(s, args[2])
			if err != nil {
				return nil, fmt.Errorf("kura_stage_attrs: %w", err)
			}
			cliArgs := []string{"stage", "episode", "--json"}
			for _, attr := range args[3:] {
				cliArgs = append(cliArgs, "--attr", attr)
			}
			cliArgs = append(cliArgs, ref, args[1], selector)
			out, errOut, runErr := b.run(s.Context(), cliArgs...)
			return func(s *script.State) (string, string, error) {
				return compactIfJSON(out), errOut, runErr
			}, nil
		},
	)

	// ── kura_stage_replace_attrs ─────────────────────────────────────────
	cmds["kura_stage_replace_attrs"] = script.Command(
		script.CmdUsage{Summary: "stage an episode replacement with attrs", Args: "<series_ref> <episode_marker> <file> <key=value...>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) < 4 {
				return nil, fmt.Errorf("kura_stage_replace_attrs: expected <ref> <ep> <file> <key=value...>")
			}
			ref := s.ExpandEnv(args[0], false)
			selector, err := inboxSelector(s, args[2])
			if err != nil {
				return nil, fmt.Errorf("kura_stage_replace_attrs: %w", err)
			}
			cliArgs := []string{"stage", "episode", "--json", "--replace"}
			for _, attr := range args[3:] {
				cliArgs = append(cliArgs, "--attr", attr)
			}
			cliArgs = append(cliArgs, ref, args[1], selector)
			out, errOut, runErr := b.run(s.Context(), cliArgs...)
			return func(s *script.State) (string, string, error) {
				return compactIfJSON(out), errOut, runErr
			}, nil
		},
	)

	// ── kura_rest_stage_attrs ────────────────────────────────────────────
	cmds["kura_rest_stage_attrs"] = script.Command(
		script.CmdUsage{Summary: "stage an episode with attrs via REST", Args: "<series_ref> <episode_marker> <file> <key=value...>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) < 4 {
				return nil, fmt.Errorf("kura_rest_stage_attrs: expected <ref> <ep> <file> <key=value...>")
			}
			ref := s.ExpandEnv(args[0], false)
			selector, err := inboxSelector(s, args[2])
			if err != nil {
				return nil, fmt.Errorf("kura_rest_stage_attrs: %w", err)
			}
			attrs, err := parseAttrArgs(args[3:])
			if err != nil {
				return nil, fmt.Errorf("kura_rest_stage_attrs: %w", err)
			}
			out, err := restStageAttrs(s.Context(), b, ref, args[1], selector, attrs)
			if err != nil {
				return nil, fmt.Errorf("kura_rest_stage_attrs: %w", err)
			}
			return staticOutput(out), nil
		},
	)

	// ── kura_stage_companions ─────────────────────────────────────────────
	cmds["kura_stage_companions"] = script.Command(
		script.CmdUsage{Summary: "stage episode with companions", Args: "<ref> <ep> <file> <c...>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) < 4 {
				return nil, fmt.Errorf("kura_stage_companions: expected <ref> <ep> <file> <companion...>")
			}
			ref := s.ExpandEnv(args[0], false)
			cliArgs := []string{"stage", "episode", "--json"}
			for _, c := range args[3:] {
				cSel, err := inboxSelector(s, c)
				if err != nil {
					return nil, fmt.Errorf("kura_stage_companions: %w", err)
				}
				cliArgs = append(cliArgs, "--companion", cSel)
			}
			mediaSel, err := inboxSelector(s, args[2])
			if err != nil {
				return nil, fmt.Errorf("kura_stage_companions: %w", err)
			}
			cliArgs = append(cliArgs, ref, args[1], mediaSel)
			out, _, err := b.run(s.Context(), cliArgs...)
			if err != nil {
				return nil, fmt.Errorf("kura_stage_companions: %w", err)
			}
			return staticOutput(out), nil
		},
	)

	// ── kura_stage_extra ──────────────────────────────────────────────────
	cmds["kura_stage_extra"] = script.Command(
		script.CmdUsage{Summary: "stage extra placement", Args: "<ref> <season> <file> [prefix]"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) < 3 || len(args) > 4 {
				return nil, fmt.Errorf("kura_stage_extra: expected <ref> <season> <file> [prefix]")
			}
			ref := s.ExpandEnv(args[0], false)
			selector, err := inboxSelector(s, args[2])
			if err != nil {
				return nil, fmt.Errorf("kura_stage_extra: %w", err)
			}
			cliArgs := []string{"stage", "extra", "--json", "--season", args[1]}
			if len(args) == 4 {
				cliArgs = append(cliArgs, "--prefix", args[3])
			}
			cliArgs = append(cliArgs, ref, selector)
			out, _, err := b.run(s.Context(), cliArgs...)
			if err != nil {
				return nil, fmt.Errorf("kura_stage_extra: %w", err)
			}
			return staticOutput(out), nil
		},
	)

	// ── kura_stage_source ─────────────────────────────────────────────────
	cmds["kura_stage_source"] = script.Command(
		script.CmdUsage{Summary: "stage with source override", Args: "<ref> <ep> <file> <source>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) != 4 {
				return nil, fmt.Errorf("kura_stage_source: expected <ref> <ep> <file> <source>")
			}
			ref := s.ExpandEnv(args[0], false)
			selector, err := inboxSelector(s, args[2])
			if err != nil {
				return nil, fmt.Errorf("kura_stage_source: %w", err)
			}
			out, _, err := b.run(s.Context(), "stage", "episode", "--json", "--source", args[3], ref, args[1], selector)
			if err != nil {
				return nil, fmt.Errorf("kura_stage_source: %w", err)
			}
			return staticOutput(out), nil
		},
	)

	// ── kura_stage_replace_source ─────────────────────────────────────────
	cmds["kura_stage_replace_source"] = script.Command(
		script.CmdUsage{Summary: "stage replacement with source", Args: "<ref> <ep> <file> <source>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) != 4 {
				return nil, fmt.Errorf("kura_stage_replace_source: expected <ref> <ep> <file> <source>")
			}
			ref := s.ExpandEnv(args[0], false)
			selector, err := inboxSelector(s, args[2])
			if err != nil {
				return nil, fmt.Errorf("kura_stage_replace_source: %w", err)
			}
			out, _, err := b.run(s.Context(), "stage", "episode", "--json", "--replace", "--source", args[3], ref, args[1], selector)
			if err != nil {
				return nil, fmt.Errorf("kura_stage_replace_source: %w", err)
			}
			return staticOutput(out), nil
		},
	)

	// ── kura_stage_series ─────────────────────────────────────────────────
	// Stage an episode using a series: media selector pointing at a
	// path inside the series root. Cross-slot stages work identically
	// to inbox: stages otherwise (need --replace if the target slot
	// already has an active record).
	cmds["kura_stage_series"] = script.Command(
		script.CmdUsage{Summary: "stage with series: media selector", Args: "<ref> <ep> <series_rel>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) != 3 {
				return nil, fmt.Errorf("kura_stage_series: expected 3 args, got %d", len(args))
			}
			ref := s.ExpandEnv(args[0], false)
			mediaSel := "series:" + filepath.ToSlash(args[2])
			out, errOut, runErr := b.run(s.Context(), "stage", "episode", "--json", ref, args[1], mediaSel)
			// Surface stderr through the WaitFunc even on failure so
			// scenarios can match against the binary's error output
			// (the script harness only sees stderr captured here, not
			// content wrapped into the returned go error).
			return func(s *script.State) (string, string, error) {
				return compactIfJSON(out), errOut, runErr
			}, nil
		},
	)

	// ── kura_stage_inplace_source ─────────────────────────────────────────
	// In-place metadata override: re-stage the active record's own file
	// using a series: media selector + an explicit source override. Only
	// valid when the episode already has an active record. Looks up the
	// active.file from kura_show so scenarios don't have to compute the
	// canonical filename.
	cmds["kura_stage_inplace_source"] = script.Command(
		script.CmdUsage{Summary: "in-place source override on active record", Args: "<ref> <ep> <source>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) != 3 {
				return nil, fmt.Errorf("kura_stage_inplace_source: expected <ref> <ep> <source>")
			}
			ref := s.ExpandEnv(args[0], false)
			mediaSel, err := lookupActiveFile(s.Context(), b, ref, args[1])
			if err != nil {
				return nil, fmt.Errorf("kura_stage_inplace_source: %w", err)
			}
			out, _, runErr := b.run(s.Context(), "stage", "episode", "--json", "--replace", "--source", args[2], ref, args[1], mediaSel)
			if runErr != nil {
				return nil, fmt.Errorf("kura_stage_inplace_source: %w", runErr)
			}
			return staticOutput(out), nil
		},
	)

	// ── kura_stage_trash ──────────────────────────────────────────────────
	cmds["kura_stage_trash"] = script.Command(
		script.CmdUsage{Summary: "stage a file for trash", Args: "<ref> <rel_path>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("kura_stage_trash: expected <ref> <rel_path>")
			}
			ref := s.ExpandEnv(args[0], false)
			// Trash takes a series: selector relative to the series root.
			trashSel := "series:" + filepath.ToSlash(args[1])
			out, _, runErr := b.run(s.Context(), "stage", "trash", "--json", ref, trashSel)
			if runErr != nil {
				return nil, fmt.Errorf("kura_stage_trash: %w", runErr)
			}
			return staticOutput(out), nil
		},
	)

	// ── kura_plan ─────────────────────────────────────────────────────────
	cmds["kura_plan"] = script.Command(
		script.CmdUsage{Summary: "compute reconcile plan", Args: "<series_ref>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) == 0 {
				return nil, fmt.Errorf("kura_plan: expected series ref")
			}
			ref := s.ExpandEnv(strings.Join(args, " "), false)
			out, _, err := b.run(s.Context(), "reconcile", "plan", "--json", ref)
			if err != nil {
				return nil, fmt.Errorf("kura_plan: %w", err)
			}
			var result map[string]any
			_ = json.Unmarshal([]byte(out), &result)
			token, _ := result["token"].(string)
			return func(s *script.State) (stdout, stderr string, err error) {
				if token != "" {
					if setErr := s.Setenv("KURA_PLAN_TOKEN", token); setErr != nil {
						return "", "", fmt.Errorf("kura_plan: set env: %w", setErr)
					}
				}
				return compactIfJSON(out), "", nil
			}, nil
		},
	)

	// ── kura_apply ────────────────────────────────────────────────────────
	cmds["kura_apply"] = script.Command(
		script.CmdUsage{Summary: "apply reconcile plan", Args: "<series_ref> <token>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("kura_apply: expected <ref> <token>")
			}
			ref := s.ExpandEnv(args[0], false)
			token := s.ExpandEnv(args[1], false)
			out, _, err := b.run(s.Context(), "reconcile", "apply", "--json", ref, token)
			if err != nil {
				return nil, fmt.Errorf("kura_apply: %w", err)
			}
			return staticOutput(out), nil
		},
	)

	// ── kura_scan ─────────────────────────────────────────────────────────
	cmds["kura_scan"] = script.Command(
		script.CmdUsage{Summary: "scan series directory", Args: "<series_ref>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) == 0 {
				return nil, fmt.Errorf("kura_scan: expected series ref")
			}
			ref := s.ExpandEnv(strings.Join(args, " "), false)
			out, _, err := b.run(s.Context(), "scan", "--json", ref)
			if err != nil {
				return nil, fmt.Errorf("kura_scan: %w", err)
			}
			return staticOutput(out), nil
		},
	)

	// ── kura_scan_refresh ─────────────────────────────────────────────────
	cmds["kura_scan_refresh"] = script.Command(
		script.CmdUsage{Summary: "refresh-scan series directory", Args: "<series_ref>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) == 0 {
				return nil, fmt.Errorf("kura_scan_refresh: expected series ref")
			}
			ref := s.ExpandEnv(strings.Join(args, " "), false)
			out, _, err := b.run(s.Context(), "scan", "--json", "--refresh", ref)
			if err != nil {
				return nil, fmt.Errorf("kura_scan_refresh: %w", err)
			}
			return staticOutput(out), nil
		},
	)

	// ── kura_reset ────────────────────────────────────────────────────────
	cmds["kura_reset"] = script.Command(
		script.CmdUsage{Summary: "reset staged episode(s)", Args: "<series_ref> [episode_marker]"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) == 0 || len(args) > 2 {
				return nil, fmt.Errorf("kura_reset: expected <series_ref> [episode_marker]")
			}
			ref := s.ExpandEnv(args[0], false)
			cliArgs := []string{"reset", "--json"}
			if len(args) == 2 {
				cliArgs = append(cliArgs, "--episode", args[1])
			} else {
				cliArgs = append(cliArgs, "--all")
			}
			cliArgs = append(cliArgs, ref)
			out, _, err := b.run(s.Context(), cliArgs...)
			if err != nil {
				return nil, fmt.Errorf("kura_reset: %w", err)
			}
			return staticOutput(out), nil
		},
	)

	// ── kura_trash_list ───────────────────────────────────────────────────
	cmds["kura_trash_list"] = script.Command(
		script.CmdUsage{Summary: "list trash for a series", Args: "<series_ref>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) == 0 {
				return nil, fmt.Errorf("kura_trash_list: expected series ref")
			}
			ref := s.ExpandEnv(strings.Join(args, " "), false)
			out, _, err := b.run(s.Context(), "trash", "list", "--json", ref)
			if err != nil {
				return nil, fmt.Errorf("kura_trash_list: %w", err)
			}
			id := firstTrashID(out)
			return func(s *script.State) (stdout, stderr string, err error) {
				if id != "" {
					if setErr := s.Setenv("KURA_TRASH_ID", id); setErr != nil {
						return "", "", fmt.Errorf("kura_trash_list: set env: %w", setErr)
					}
				}
				return compactIfJSON(out), "", nil
			}, nil
		},
	)

	// ── kura_trash_restore ────────────────────────────────────────────────
	cmds["kura_trash_restore"] = script.Command(
		script.CmdUsage{Summary: "restore a trash entry", Args: "<series_ref> <trash_id>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("kura_trash_restore: expected <series_ref> <trash_id>")
			}
			ref := s.ExpandEnv(args[0], false)
			id := s.ExpandEnv(args[1], false)
			out, _, err := b.run(s.Context(), "trash", "restore", "--json", ref, id)
			if err != nil {
				return nil, fmt.Errorf("kura_trash_restore: %w", err)
			}
			return staticOutput(out), nil
		},
	)

	// ── kura_first_trash_id ───────────────────────────────────────────────
	// Reads the first trash entry's ULID for a series via kura trash list
	// --json and stashes it into $KURA_FIRST_TRASH_ID. Lets scenarios
	// reference the bucket dir on disk without parsing JSON in the script.
	cmds["kura_first_trash_id"] = script.Command(
		script.CmdUsage{Summary: "set $KURA_FIRST_TRASH_ID for series", Args: "<ref>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("kura_first_trash_id: expected 1 arg, got %d", len(args))
			}
			ref := s.ExpandEnv(args[0], false)
			out, _, err := b.run(s.Context(), "trash", "list", "--json", ref)
			if err != nil {
				return nil, fmt.Errorf("kura_first_trash_id: %w", err)
			}
			var doc struct {
				Series []struct {
					Entries []struct {
						ID string `json:"id"`
					} `json:"entries"`
				} `json:"series"`
			}
			if err := json.Unmarshal([]byte(out), &doc); err != nil {
				return nil, fmt.Errorf("kura_first_trash_id: decode: %w", err)
			}
			if len(doc.Series) == 0 || len(doc.Series[0].Entries) == 0 {
				return nil, fmt.Errorf("kura_first_trash_id: no trash entries for %s", ref)
			}
			id := doc.Series[0].Entries[0].ID
			return func(s *script.State) (string, string, error) {
				if setErr := s.Setenv("KURA_FIRST_TRASH_ID", id); setErr != nil {
					return "", "", fmt.Errorf("kura_first_trash_id: set env: %w", setErr)
				}
				return id + "\n", "", nil
			}, nil
		},
	)

	// ── kura_trash_empty_all ──────────────────────────────────────────────
	// Library-wide variant. Surfaces stderr + non-zero exit through the
	// WaitFunc so scenarios can match per-series Failure messages even
	// when the underlying command exits non-zero.
	cmds["kura_trash_empty_all"] = script.Command(
		script.CmdUsage{Summary: "empty trash across the library", Args: "[older_than]"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			cliArgs := []string{"trash", "empty", "--all", "--confirm"}
			if len(args) >= 1 {
				cliArgs = append(cliArgs, "--older-than", args[0])
			}
			out, errOut, runErr := b.run(s.Context(), cliArgs...)
			return func(s *script.State) (string, string, error) {
				return out, errOut, runErr
			}, nil
		},
	)

	// ── kura_trash_empty ──────────────────────────────────────────────────
	cmds["kura_trash_empty"] = script.Command(
		script.CmdUsage{Summary: "empty trash for a series", Args: "<series_ref> [older_than]"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) == 0 {
				return nil, fmt.Errorf("kura_trash_empty: expected series ref")
			}
			ref := s.ExpandEnv(args[0], false)
			cliArgs := []string{"trash", "empty", "--json"}
			if len(args) >= 2 {
				cliArgs = append(cliArgs, "--older-than", args[1])
			}
			cliArgs = append(cliArgs, ref)
			out, _, err := b.run(s.Context(), cliArgs...)
			if err != nil {
				return nil, fmt.Errorf("kura_trash_empty: %w", err)
			}
			return staticOutput(out), nil
		},
	)

	// ── kura_reindex ──────────────────────────────────────────────────────
	cmds["kura_reindex"] = script.Command(
		script.CmdUsage{Summary: "rebuild library index", Args: ""},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if _, _, err := b.run(s.Context(), "reindex"); err != nil {
				return nil, fmt.Errorf("kura_reindex: %w", err)
			}
			// REST returns 204 No Content; older in-process harness
			// printed {"ok":true} as a stable success signal for
			// scenario matchers. Preserve that contract.
			return staticOutput(`{"ok":true}` + "\n"), nil
		},
	)

	// ── kura_list ─────────────────────────────────────────────────────────
	cmds["kura_list"] = script.Command(
		script.CmdUsage{Summary: "list tracked series", Args: ""},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			out, _, err := b.run(s.Context(), "list", "--json")
			if err != nil {
				return nil, fmt.Errorf("kura_list: %w", err)
			}
			return staticOutput(out), nil
		},
	)

	// ── kura_list_tags ───────────────────────────────────────────────────
	cmds["kura_list_tags"] = script.Command(
		script.CmdUsage{Summary: "list series filtered by tags", Args: "<tag_expression...>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) == 0 {
				return nil, fmt.Errorf("kura_list_tags: expected <tag_expression...>")
			}
			cliArgs := []string{"list", "--json"}
			for _, tag := range args {
				cliArgs = append(cliArgs, "--tag", tag)
			}
			out, errOut, runErr := b.run(s.Context(), cliArgs...)
			return func(s *script.State) (string, string, error) {
				return compactIfJSON(out), errOut, runErr
			}, nil
		},
	)

	// ── kura_remove ───────────────────────────────────────────────────────
	cmds["kura_remove"] = script.Command(
		script.CmdUsage{Summary: "untrack a series", Args: "<series_ref>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) == 0 {
				return nil, fmt.Errorf("kura_remove: expected series ref")
			}
			ref := s.ExpandEnv(strings.Join(args, " "), false)
			out, _, err := b.run(s.Context(), "remove", "--json", ref)
			if err != nil {
				return nil, fmt.Errorf("kura_remove: %w", err)
			}
			return staticOutput(out), nil
		},
	)

	// ── kura_series_dir ───────────────────────────────────────────────────
	cmds["kura_series_dir"] = script.Command(
		script.CmdUsage{Summary: "compute series dir, set $KURA_SERIES_DIR", Args: "<ref>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) == 0 {
				return nil, fmt.Errorf("kura_series_dir: expected ref (metadata or series)")
			}
			input := s.ExpandEnv(strings.Join(args, " "), false)
			seriesRefStr, err := resolveSeriesRefForFixture(b, input)
			if err != nil {
				return nil, fmt.Errorf("kura_series_dir: %w", err)
			}
			ref, err := refs.ParseSeries(seriesRefStr)
			if err != nil {
				return nil, fmt.Errorf("kura_series_dir: %w", err)
			}
			dir := api.SeriesDir(b.libRoot, ref)
			return func(s *script.State) (stdout, stderr string, err error) {
				if setErr := s.Setenv("KURA_SERIES_DIR", dir); setErr != nil {
					return "", "", fmt.Errorf("kura_series_dir: set env: %w", setErr)
				}
				return dir + "\n", "", nil
			}, nil
		},
	)

	// ── kura_assert_lib_mode ──────────────────────────────────────────────
	cmds["kura_assert_lib_mode"] = script.Command(
		script.CmdUsage{Summary: "assert mode for path under library root", Args: "<octal_mode> <rel_path>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) < 2 {
				return nil, fmt.Errorf("kura_assert_lib_mode: expected <octal_mode> <rel_path>")
			}
			mode, err := parseModeArg(args[0])
			if err != nil {
				return nil, fmt.Errorf("kura_assert_lib_mode: %w", err)
			}
			rel := s.ExpandEnv(strings.Join(args[1:], " "), false)
			path := filepath.Join(b.libRoot, filepath.FromSlash(rel))
			if err := assertMode(path, mode); err != nil {
				return nil, fmt.Errorf("kura_assert_lib_mode: %w", err)
			}
			return staticOutput(""), nil
		},
	)

	// ── kura_assert_lib_glob_mode ─────────────────────────────────────────
	cmds["kura_assert_lib_glob_mode"] = script.Command(
		script.CmdUsage{Summary: "assert mode for paths matching a library-root glob", Args: "<octal_mode> <glob>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) < 2 {
				return nil, fmt.Errorf("kura_assert_lib_glob_mode: expected <octal_mode> <glob>")
			}
			mode, err := parseModeArg(args[0])
			if err != nil {
				return nil, fmt.Errorf("kura_assert_lib_glob_mode: %w", err)
			}
			rel := s.ExpandEnv(strings.Join(args[1:], " "), false)
			matches, err := filepath.Glob(filepath.Join(b.libRoot, filepath.FromSlash(rel)))
			if err != nil {
				return nil, fmt.Errorf("kura_assert_lib_glob_mode: %w", err)
			}
			if len(matches) == 0 {
				return nil, fmt.Errorf("kura_assert_lib_glob_mode: no matches for %q", rel)
			}
			for _, path := range matches {
				if err := assertMode(path, mode); err != nil {
					return nil, fmt.Errorf("kura_assert_lib_glob_mode: %w", err)
				}
			}
			return staticOutput(""), nil
		},
	)

	// ── kura_assert_series_mode ───────────────────────────────────────────
	cmds["kura_assert_series_mode"] = script.Command(
		script.CmdUsage{Summary: "assert mode for path under a series root", Args: "<ref> <octal_mode> <rel_path>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) < 3 {
				return nil, fmt.Errorf("kura_assert_series_mode: expected <ref> <octal_mode> <rel_path>")
			}
			input := s.ExpandEnv(args[0], false)
			mode, err := parseModeArg(args[1])
			if err != nil {
				return nil, fmt.Errorf("kura_assert_series_mode: %w", err)
			}
			seriesRoot, err := seriesRootForFixture(b, input)
			if err != nil {
				return nil, fmt.Errorf("kura_assert_series_mode: %w", err)
			}
			rel := s.ExpandEnv(strings.Join(args[2:], " "), false)
			path := filepath.Join(seriesRoot, filepath.FromSlash(rel))
			if err := assertMode(path, mode); err != nil {
				return nil, fmt.Errorf("kura_assert_series_mode: %w", err)
			}
			return staticOutput(""), nil
		},
	)

	// ── kura_assert_series_glob_mode ──────────────────────────────────────
	cmds["kura_assert_series_glob_mode"] = script.Command(
		script.CmdUsage{Summary: "assert mode for paths matching a series-root glob", Args: "<ref> <octal_mode> <glob>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) < 3 {
				return nil, fmt.Errorf("kura_assert_series_glob_mode: expected <ref> <octal_mode> <glob>")
			}
			input := s.ExpandEnv(args[0], false)
			mode, err := parseModeArg(args[1])
			if err != nil {
				return nil, fmt.Errorf("kura_assert_series_glob_mode: %w", err)
			}
			seriesRoot, err := seriesRootForFixture(b, input)
			if err != nil {
				return nil, fmt.Errorf("kura_assert_series_glob_mode: %w", err)
			}
			rel := s.ExpandEnv(strings.Join(args[2:], " "), false)
			matches, err := filepath.Glob(filepath.Join(seriesRoot, filepath.FromSlash(rel)))
			if err != nil {
				return nil, fmt.Errorf("kura_assert_series_glob_mode: %w", err)
			}
			if len(matches) == 0 {
				return nil, fmt.Errorf("kura_assert_series_glob_mode: no matches for %q", rel)
			}
			for _, path := range matches {
				if err := assertMode(path, mode); err != nil {
					return nil, fmt.Errorf("kura_assert_series_glob_mode: %w", err)
				}
			}
			return staticOutput(""), nil
		},
	)

	// ── kura_assert_active_mode ───────────────────────────────────────────
	cmds["kura_assert_active_mode"] = script.Command(
		script.CmdUsage{Summary: "assert mode for an episode's active media file", Args: "<ref> <episode> <octal_mode>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) != 3 {
				return nil, fmt.Errorf("kura_assert_active_mode: expected <ref> <episode> <octal_mode>")
			}
			ref := s.ExpandEnv(args[0], false)
			mode, err := parseModeArg(args[2])
			if err != nil {
				return nil, fmt.Errorf("kura_assert_active_mode: %w", err)
			}
			path, err := activeFilePath(s.Context(), b, ref, args[1])
			if err != nil {
				return nil, fmt.Errorf("kura_assert_active_mode: %w", err)
			}
			if err := assertMode(path, mode); err != nil {
				return nil, fmt.Errorf("kura_assert_active_mode: %w", err)
			}
			return staticOutput(""), nil
		},
	)

	// ── kura_assert_active_group_parent ───────────────────────────────────
	cmds["kura_assert_active_group_parent"] = script.Command(
		script.CmdUsage{Summary: "assert active media group matches its parent directory", Args: "<ref> <episode>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("kura_assert_active_group_parent: expected <ref> <episode>")
			}
			ref := s.ExpandEnv(args[0], false)
			path, err := activeFilePath(s.Context(), b, ref, args[1])
			if err != nil {
				return nil, fmt.Errorf("kura_assert_active_group_parent: %w", err)
			}
			if err := assertGroupMatchesParent(path); err != nil {
				return nil, fmt.Errorf("kura_assert_active_group_parent: %w", err)
			}
			return staticOutput(""), nil
		},
	)

	// ── kura_mkfile ───────────────────────────────────────────────────────
	cmds["kura_mkfile"] = script.Command(
		script.CmdUsage{Summary: "create empty file in workdir", Args: "<path>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("kura_mkfile: expected 1 arg")
			}
			absPath := s.Path(args[0])
			if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
				return nil, fmt.Errorf("kura_mkfile: mkdir: %w", err)
			}
			if err := os.WriteFile(absPath, nil, 0o644); err != nil {
				return nil, fmt.Errorf("kura_mkfile: %w", err)
			}
			return staticOutput(""), nil
		},
	)

	// ── kura_write_series_file ────────────────────────────────────────────
	cmds["kura_write_series_file"] = script.Command(
		script.CmdUsage{Summary: "create empty file inside series dir", Args: "<ref> <rel_path>"},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) < 2 {
				return nil, fmt.Errorf("kura_write_series_file: expected <ref> <rel_path>")
			}
			input := s.ExpandEnv(args[0], false)
			seriesRefStr, err := resolveSeriesRefForFixture(b, input)
			if err != nil {
				return nil, fmt.Errorf("kura_write_series_file: %w", err)
			}
			ref, err := refs.ParseSeries(seriesRefStr)
			if err != nil {
				return nil, fmt.Errorf("kura_write_series_file: %w", err)
			}
			relPath := strings.Join(args[1:], " ")
			absPath := filepath.Join(api.SeriesDir(b.libRoot, ref), relPath)
			if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
				return nil, fmt.Errorf("kura_write_series_file: mkdir: %w", err)
			}
			if err := os.WriteFile(absPath, nil, 0o644); err != nil {
				return nil, fmt.Errorf("kura_write_series_file: %w", err)
			}
			return staticOutput(""), nil
		},
	)

	eng := &script.Engine{
		Cmds:  cmds,
		Conds: script.DefaultConds(),
	}
	return eng
}

func parseModeArg(raw string) (fs.FileMode, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(raw), 8, 32)
	if err != nil {
		return 0, fmt.Errorf("parse mode %q: %w", raw, err)
	}
	return fs.FileMode(parsed), nil
}

func assertMode(path string, want fs.FileMode) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if got := info.Mode().Perm(); got != want {
		return fmt.Errorf("%s mode = %#o, want %#o", path, got, want)
	}
	return nil
}

func assertGroupMatchesParent(path string) error {
	fileGID, err := statGID(path)
	if err != nil {
		return err
	}
	parentGID, err := statGID(filepath.Dir(path))
	if err != nil {
		return err
	}
	if fileGID != parentGID {
		return fmt.Errorf("%s gid = %d, parent gid = %d", path, fileGID, parentGID)
	}
	return nil
}

func statGID(path string) (uint64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	value := reflect.ValueOf(info.Sys())
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	field := value.FieldByName("Gid")
	if !field.IsValid() {
		return 0, fmt.Errorf("%s: stat does not expose gid on this platform", path)
	}
	return field.Convert(reflect.TypeOf(uint64(0))).Uint(), nil
}

// resolveSeriesRefForFixture maps either a metadata ref
// (provider:id) or a literal series ref to the underlying SeriesRef
// the harness needs to compute on-disk paths. Metadata refs trigger
// a /api/v1/series/{ref} round-trip and read `.ref` from the
// response. Literal series refs pass through unchanged. Used by the
// fixture commands kura_series_dir and kura_write_series_file so
// scenarios can keep passing $KURA_LAST_REF after the metadata-ref
// migration.
func resolveSeriesRefForFixture(b *e2eBinary, raw string) (string, error) {
	if !strings.Contains(raw, ":") {
		return raw, nil
	}
	resp, err := http.Get(b.url + "/api/v1/series/" + raw)
	if err != nil {
		return "", fmt.Errorf("fixture lookup %q: %w", raw, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fixture lookup %q: status %d", raw, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("fixture lookup %q: read: %w", raw, err)
	}
	var doc struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", fmt.Errorf("fixture lookup %q: decode: %w", raw, err)
	}
	if doc.Ref == "" {
		return "", fmt.Errorf("fixture lookup %q: response missing ref", raw)
	}
	return doc.Ref, nil
}

func seriesRootForFixture(b *e2eBinary, raw string) (string, error) {
	seriesRefStr, err := resolveSeriesRefForFixture(b, raw)
	if err != nil {
		return "", err
	}
	ref, err := refs.ParseSeries(seriesRefStr)
	if err != nil {
		return "", err
	}
	return api.SeriesDir(b.libRoot, ref), nil
}

// lookupActiveFile fetches kura_show for one episode and returns the
// active.file (a `series:<rel>` selector). Errors when the episode
// has no active record. Used by kura_stage_inplace_source to locate
// the on-disk path the in-place override targets.
func lookupActiveFile(ctx context.Context, b *e2eBinary, ref, episode string) (string, error) {
	out, _, err := b.run(ctx, "show", "--json", "--episodes", episode, ref)
	if err != nil {
		return "", fmt.Errorf("show: %w", err)
	}
	var doc struct {
		Seasons []struct {
			Episodes []struct {
				Episode string `json:"episode"`
				Active  *struct {
					File string `json:"file"`
				} `json:"active"`
			} `json:"episodes"`
		} `json:"seasons"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		return "", fmt.Errorf("decode show: %w (out=%s)", err, out)
	}
	for _, season := range doc.Seasons {
		for _, ep := range season.Episodes {
			if ep.Active != nil && (ep.Episode == episode || strings.EqualFold(ep.Episode, episode)) {
				return ep.Active.File, nil
			}
		}
	}
	return "", fmt.Errorf("episode %q has no active record in show response", episode)
}

func activeFilePath(ctx context.Context, b *e2eBinary, ref, episode string) (string, error) {
	selector, err := lookupActiveFile(ctx, b, ref, episode)
	if err != nil {
		return "", err
	}
	const prefix = "series:"
	if !strings.HasPrefix(selector, prefix) {
		return "", fmt.Errorf("active selector %q is not series-relative", selector)
	}
	seriesRoot, err := seriesRootForFixture(b, ref)
	if err != nil {
		return "", err
	}
	return filepath.Join(seriesRoot, filepath.FromSlash(strings.TrimPrefix(selector, prefix))), nil
}

func parseAttrArgs(args []string) (map[string]string, error) {
	attrs := make(map[string]string, len(args))
	for _, arg := range args {
		key, value, ok := strings.Cut(arg, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("attr %q must be key=value", arg)
		}
		if _, exists := attrs[key]; exists {
			return nil, fmt.Errorf("attr %q specified more than once", key)
		}
		attrs[key] = value
	}
	return attrs, nil
}

func restStageAttrs(ctx context.Context, b *e2eBinary, ref, episode, media string, attrs map[string]string) (string, error) {
	body := map[string]any{
		"episodes": []map[string]any{{
			"episode": episode,
			"media":   media,
			"attrs":   attrs,
		}},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.url+"/api/v1/series/"+ref+"/stage", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusAccepted {
		return "", fmt.Errorf("stage status %d: %s", resp.StatusCode, data)
	}
	var ack struct {
		JobID string `json:"jobId"`
	}
	if err := json.Unmarshal(data, &ack); err != nil {
		return "", err
	}
	if ack.JobID == "" {
		return "", fmt.Errorf("stage response missing jobId: %s", data)
	}
	result, err := pollRESTJob(ctx, b, ack.JobID)
	if err != nil {
		return "", err
	}
	return compactIfJSON(string(result)), nil
}

func pollRESTJob(ctx context.Context, b *e2eBinary, jobID string) ([]byte, error) {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(5 * time.Second)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.url+"/api/v1/jobs/"+jobID, http.NoBody)
		if err != nil {
			return nil, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("job status %d: %s", resp.StatusCode, data)
		}
		var status struct {
			State  string          `json:"state"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(data, &status); err != nil {
			return nil, err
		}
		switch status.State {
		case "succeeded":
			return status.Result, nil
		case "failed":
			if status.Error != nil {
				return nil, fmt.Errorf("job failed: %s", status.Error.Message)
			}
			return nil, fmt.Errorf("job failed")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline:
			return nil, fmt.Errorf("job %s did not finish", jobID)
		case <-ticker.C:
		}
	}
}

// staticOutput wraps a fixed stdout in a script.WaitFunc, compacting
// JSON output so scenario matchers like `"status":"staged"` (no space
// after colon) match what the CLI's pretty-printer emits.
func staticOutput(out string) script.WaitFunc {
	return func(s *script.State) (stdout, stderr string, err error) {
		return compactIfJSON(out), "", nil
	}
}

// compactIfJSON returns a single-line JSON serialization when the
// input is decodable JSON; otherwise returns input unchanged. Pretty-
// printed CLI output gets normalized so scenario matchers stay
// transport-agnostic.
func compactIfJSON(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return s
	}
	if trimmed[0] != '{' && trimmed[0] != '[' {
		return s
	}
	var dst bytes.Buffer
	if err := json.Compact(&dst, []byte(trimmed)); err != nil {
		return s
	}
	dst.WriteByte('\n')
	return dst.String()
}

// firstTrashID extracts the first ULID from a kura trash list JSON
// response. Mirrors the per-series Series[0].Entries[0].ID lookup the
// in-process harness used.
func firstTrashID(jsonOut string) string {
	var resp struct {
		Series []struct {
			Entries []struct {
				ID string `json:"id"`
			} `json:"entries"`
		} `json:"series"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &resp); err != nil {
		return ""
	}
	if len(resp.Series) == 0 {
		return ""
	}
	if len(resp.Series[0].Entries) == 0 {
		return ""
	}
	return resp.Series[0].Entries[0].ID
}
