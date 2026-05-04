package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/errkind"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/workflow"
)

type reconcilePlanInput struct {
	Ref string `json:"ref" jsonschema:"Metadata ref (e.g. \"tvdb:370070\") from kura_resolve."`
}

type mcpReconcilePlan struct {
	Token     string               `json:"token,omitempty"`
	ExpiresAt string               `json:"expiresAt,omitempty"`
	Changes   []mcpReconcileChange `json:"changes"`
}

type mcpReconcileChange struct {
	Kind       string             `json:"kind"`
	Episode    string             `json:"episode"`
	From       string             `json:"from"`
	To         string             `json:"to"`
	Source     string             `json:"source,omitempty"`
	Resolution string             `json:"resolution,omitempty"`
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
	Companions []mcpReconcileMove `json:"companions,omitempty"`
}

const reconcilePlanDescription = `Compute the file moves ` + "`kura_reconcile_apply`" + ` would perform on this series and persist them as a plan. Returns the plan's ` + "`token`" + ` plus the changes it would make. Pass ` + "`token`" + ` to ` + "`kura_reconcile_apply`" + ` to execute. Plans expire 5 minutes after creation; re-call to get a fresh one.

A change describes one media-file move: kind (add / replace / refresh), episode slot, current path (` + "`from`" + `), target canonical path (` + "`to`" + `), source / resolution, and any companion-file moves. ` + "`replaced`" + ` is set when the move displaces an existing active record.

Returns ` + "`changes: []`" + ` and no token when there is nothing to do.`

func addReconcilePlanTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_reconcile_plan",
		Title:       "Plan reconcile",
		Description: reconcilePlanDescription,
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
		return nil, projectReconcilePlan(full, seriesRoot), nil
	})
}

// projectReconcilePlan flattens the on-disk ReconcilePlan into the
// MCP shape: drops createdAt + plan.series wrapper, relativizes paths
// that fall under seriesRoot (external staging paths stay absolute as
// a signal), and strips replaced.to (trash path is invisible to the
// agent — the file is "gone" as far as MCP is concerned).
func projectReconcilePlan(in response.ReconcilePlan, seriesRoot string) mcpReconcilePlan {
	out := mcpReconcilePlan{
		Token:   in.Token,
		Changes: make([]mcpReconcileChange, 0, len(in.Plan.Changes)),
	}
	if in.ExpiresAt != nil {
		out.ExpiresAt = in.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	for _, change := range in.Plan.Changes {
		mc := mcpReconcileChange{
			Kind:       change.Kind,
			Episode:    change.Episode.String(),
			From:       relativizeUnderRoot(change.From, seriesRoot),
			To:         relativizeUnderRoot(change.To, seriesRoot),
			Source:     change.Source,
			Resolution: change.Resolution,
		}
		if len(change.Companions) > 0 {
			mc.Companions = make([]mcpReconcileMove, 0, len(change.Companions))
			for _, comp := range change.Companions {
				mc.Companions = append(mc.Companions, mcpReconcileMove{
					From: relativizeUnderRoot(comp.From, seriesRoot),
					To:   relativizeUnderRoot(comp.To, seriesRoot),
				})
			}
		}
		if change.Replaced != nil {
			rep := &mcpReplaced{
				From:       relativizeUnderRoot(change.Replaced.From, seriesRoot),
				Source:     change.Replaced.Source,
				Resolution: change.Replaced.Resolution,
			}
			if len(change.Replaced.Companions) > 0 {
				rep.Companions = make([]mcpReconcileMove, 0, len(change.Replaced.Companions))
				for _, comp := range change.Replaced.Companions {
					rep.Companions = append(rep.Companions, mcpReconcileMove{
						From: relativizeUnderRoot(comp.From, seriesRoot),
						To:   relativizeUnderRoot(comp.To, seriesRoot),
					})
				}
			}
			mc.Replaced = rep
		}
		out.Changes = append(out.Changes, mc)
	}
	return out
}

// relativizeUnderRoot returns path relative to root when path is
// under root; otherwise path verbatim. Already-relative paths pass
// through unchanged. Used by reconcile plan projection to keep
// in-series moves short while preserving external staging paths as
// an "outside the series" signal.
func relativizeUnderRoot(path, root string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		return path
	}
	rootClean := filepath.Clean(root) + string(filepath.Separator)
	if !strings.HasPrefix(path, rootClean) {
		return path
	}
	return path[len(rootClean):]
}
