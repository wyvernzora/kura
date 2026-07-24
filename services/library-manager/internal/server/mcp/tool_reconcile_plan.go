package mcp

import (
	"context"
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/errkind"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/paths"
	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
	"github.com/wyvernzora/kura/services/library-manager/pkg/api"
)

type reconcilePlanInput struct {
	Ref string `json:"ref" jsonschema:"Metadata ref, e.g. \"tvdb:370070\"."`
}

type mcpReconcilePlan struct {
	Token      string               `json:"token,omitempty"`
	Changes    []mcpReconcileChange `json:"changes"`
	TrashItems []mcpReconcileTrash  `json:"trashItems,omitempty"`
	Extras     []mcpReconcileExtra  `json:"extras,omitempty"`
}

// mcpReconcileTrash is the agent-facing projection of a standalone
// stagedTrash entry. The file at `from` will disappear at apply time.
// Bucket structure (where it lands inside .kura/trash/) is
// intentionally not exposed. Source / Resolution / Codec are empty
// for standalone trash (no mediainfo probe at stage time); Size and
// MTime come from the file stat.
type mcpReconcileTrash struct {
	ID         string   `json:"id"`
	From       string   `json:"from"`
	Companions []string `json:"companions,omitempty"`
	Source     string   `json:"source,omitempty"`
	Resolution string   `json:"resolution,omitempty"`
	Codec      string   `json:"codec,omitempty"`
	Size       int64    `json:"size,omitempty"`
	MTime      string   `json:"mtime,omitempty"`
}

type mcpReconcileExtra struct {
	ID     string `json:"id"`
	Season int    `json:"season"`
	From   string `json:"from"`
	To     string `json:"to"`
	Prefix string `json:"prefix,omitempty"`
	IsDir  bool   `json:"isDir,omitempty"`
}

type mcpReconcileChange struct {
	Kind       string             `json:"kind"`
	Episode    string             `json:"episode"`
	From       string             `json:"from"`
	To         string             `json:"to"`
	Source     string             `json:"source,omitempty"`
	Resolution string             `json:"resolution,omitempty"`
	Codec      string             `json:"codec,omitempty"`
	Size       int64              `json:"size,omitempty"`
	MTime      string             `json:"mtime,omitempty"`
	Companions []mcpReconcileMove `json:"companions,omitempty"`
	Replaced   *mcpReplaced       `json:"replaced,omitempty"`
}

type mcpReconcileMove struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type mcpReplaced struct {
	From       string             `json:"from"`
	Source     string             `json:"source,omitempty"`
	Resolution string             `json:"resolution,omitempty"`
	Codec      string             `json:"codec,omitempty"`
	Size       int64              `json:"size,omitempty"`
	MTime      string             `json:"mtime,omitempty"`
	Companions []mcpReconcileMove `json:"companions,omitempty"`
}

//go:embed tool_reconcile_plan.md
var toolReconcilePlanDoc string

func addReconcilePlanTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_reconcile_plan",
		Title:       "Plan reconcile",
		Description: forLLM(toolReconcilePlanDoc),
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint:    false,
			IdempotentHint:  true,
			OpenWorldHint:   &hintFalse,
			DestructiveHint: &hintFalse,
		},
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in reconcilePlanInput) (*sdkmcp.CallToolResult, any, error) {
		metaRef, err := refs.ParseMetadata(in.Ref)
		if err != nil {
			return toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: fmt.Sprintf("kura_reconcile_plan: %v", err),
			}), nil, nil
		}
		seriesRef, ok, err := deps.Workflow.Index.Get(metaRef)
		if err != nil {
			return toolErrorResult(err), nil, nil
		}
		if !ok {
			return toolErrorResult(&workflow.MetadataRefNotIndexedError{Ref: metaRef}), nil, nil
		}
		full, err := workflow.PlanReconcile(ctx, deps.Workflow, workflow.PlanReconcileInput{Ref: seriesRef})
		if err != nil {
			return toolErrorResult(err), nil, nil
		}
		seriesRoot := paths.SeriesDir(deps.Workflow.LibRoot, seriesRef)
		return nil, projectReconcilePlan(full, seriesRoot, deps.Workflow.InboxRoot), nil
	})
}

// projectReconcilePlan flattens the on-disk ReconcilePlan into the
// MCP shape: drops createdAt + plan.series wrapper, relativizes paths
// that fall under seriesRoot, turns inbox paths into inbox: selectors,
// redacts other absolute paths to basenames, and strips replaced.to
// (trash path is invisible to the
// agent — the file is "gone" as far as MCP is concerned).
func projectReconcilePlan(in api.ReconcilePlan, seriesRoot, inboxRoot string) mcpReconcilePlan {
	out := mcpReconcilePlan{
		Token:   in.Token,
		Changes: make([]mcpReconcileChange, 0, len(in.Plan.Changes)),
	}
	for _, change := range in.Plan.Changes {
		mc := mcpReconcileChange{
			Kind:       change.Kind,
			Episode:    change.Episode.String(),
			From:       projectReconcilePreviewPath(change.From, seriesRoot, inboxRoot),
			To:         projectReconcilePreviewPath(change.To, seriesRoot, inboxRoot),
			Source:     change.Source,
			Resolution: change.Resolution,
			Codec:      change.Codec,
			Size:       change.Size,
			MTime:      formatMTime(change.MTime),
		}
		if len(change.Companions) > 0 {
			mc.Companions = make([]mcpReconcileMove, 0, len(change.Companions))
			for _, comp := range change.Companions {
				mc.Companions = append(mc.Companions, mcpReconcileMove{
					From: projectReconcilePreviewPath(comp.From, seriesRoot, inboxRoot),
					To:   projectReconcilePreviewPath(comp.To, seriesRoot, inboxRoot),
				})
			}
		}
		if change.Replaced != nil {
			rep := &mcpReplaced{
				From:       projectReconcilePreviewPath(change.Replaced.From, seriesRoot, inboxRoot),
				Source:     change.Replaced.Source,
				Resolution: change.Replaced.Resolution,
				Codec:      change.Replaced.Codec,
				Size:       change.Replaced.Size,
				MTime:      formatMTime(change.Replaced.MTime),
			}
			if len(change.Replaced.Companions) > 0 {
				rep.Companions = make([]mcpReconcileMove, 0, len(change.Replaced.Companions))
				for _, comp := range change.Replaced.Companions {
					rep.Companions = append(rep.Companions, mcpReconcileMove{
						From: projectReconcilePreviewPath(comp.From, seriesRoot, inboxRoot),
						To:   projectReconcilePreviewPath(comp.To, seriesRoot, inboxRoot),
					})
				}
			}
			mc.Replaced = rep
		}
		out.Changes = append(out.Changes, mc)
	}
	if len(in.Plan.TrashItems) > 0 {
		out.TrashItems = make([]mcpReconcileTrash, 0, len(in.Plan.TrashItems))
		for _, item := range in.Plan.TrashItems {
			companions := make([]string, 0, len(item.Companions))
			for _, companion := range item.Companions {
				companions = append(companions, projectReconcilePreviewPath(companion.From, seriesRoot, inboxRoot))
			}
			out.TrashItems = append(out.TrashItems, mcpReconcileTrash{
				ID:         item.ID,
				From:       projectReconcilePreviewPath(item.From, seriesRoot, inboxRoot),
				Companions: companions,
				Source:     item.Source,
				Resolution: item.Resolution,
				Codec:      item.Codec,
				Size:       item.Size,
				MTime:      formatMTime(item.MTime),
			})
		}
	}
	if len(in.Plan.Extras) > 0 {
		out.Extras = make([]mcpReconcileExtra, 0, len(in.Plan.Extras))
		for _, item := range in.Plan.Extras {
			out.Extras = append(out.Extras, mcpReconcileExtra{
				ID:     item.ID,
				Season: item.Season,
				From:   projectReconcilePreviewPath(item.From, seriesRoot, inboxRoot),
				To:     projectReconcilePreviewPath(item.To, seriesRoot, inboxRoot),
				Prefix: item.Prefix,
				IsDir:  item.IsDir,
			})
		}
	}
	return out
}

// formatMTime returns the RFC3339 (UTC) string for a non-nil MTime,
// empty string otherwise.
func formatMTime(mt *time.Time) string {
	if mt == nil {
		return ""
	}
	return mt.UTC().Format("2006-01-02T15:04:05Z07:00")
}

func projectReconcilePreviewPath(path, seriesRoot, inboxRoot string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		return path
	}
	if rel, ok := relativizeUnderRoot(path, seriesRoot); ok {
		return rel
	}
	if rel, ok := relativizeUnderRoot(path, inboxRoot); ok {
		return "inbox:" + filepath.ToSlash(rel)
	}
	return filepath.Base(path)
}

// relativizeUnderRoot returns path relative to root when path is
// under root.
func relativizeUnderRoot(path, root string) (string, bool) {
	if root == "" || path == "" || !filepath.IsAbs(path) {
		return "", false
	}
	cleanRoot := filepath.Clean(root)
	if path == cleanRoot {
		return ".", true
	}
	rootClean := cleanRoot + string(filepath.Separator)
	if !strings.HasPrefix(path, rootClean) {
		return "", false
	}
	return path[len(rootClean):], true
}
