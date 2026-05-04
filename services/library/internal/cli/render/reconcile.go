package render

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/wyvernzora/kura/internal/cli/style"
	"github.com/wyvernzora/kura/internal/response"
)

// PlanReconcile writes the reconcile plan response. asJSON toggles JSON
// output; otherwise emits a styled FROM→TO table followed by the token
// and expiry summary.
func PlanReconcile(w io.Writer, result response.ReconcilePlan, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	moves := flattenPlanMoves(result.Plan.Changes)
	tty := style.ShouldStyle(w)
	if len(moves) == 0 {
		if tty {
			if _, err := fmt.Fprintln(w, "\nNothing to reconcile."); err != nil {
				return err
			}
		}
		return nil
	}
	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"KIND", "FROM", "TO"})
	tw.SetStyle(style.BorderlessTableStyle())
	tw.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1},
		{Number: 2},
		{Number: 3},
	})
	for _, move := range moves {
		tw.AppendRow(table.Row{"FILE", move.From, move.To})
	}
	if err := style.WriteStyledTable(w, tw, nil); err != nil {
		return err
	}
	if result.Token == "" || result.ExpiresAt == nil {
		return nil
	}
	_, err := fmt.Fprintf(w, "Token: %s\nExpiresAt: %s\n", result.Token, result.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"))
	return err
}

// ApplyReconcile writes the apply response. asJSON toggles JSON output;
// otherwise emits a multi-line summary that surfaces partial-progress
// detail when a step failed.
func ApplyReconcile(w io.Writer, result response.ReconcileApply, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	if result.FailedStep == nil {
		_, err := fmt.Fprintf(w, "Applied %d of %d steps\n", result.AppliedSteps, result.TotalSteps)
		return err
	}
	step := result.FailedStep
	target := step.Path
	if target == "" {
		target = step.From + " -> " + step.To
	}
	_, err := fmt.Fprintf(w,
		"Applied %d of %d steps; failed at step %s (%s, %s, %s): %s\n",
		result.AppliedSteps, result.TotalSteps, step.ID, step.Kind, step.OwnerKind, target, step.ErrMessage,
	)
	return err
}

func RecoverReconcile(w io.Writer, result response.RecoverReconcile, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	if !result.Cleared {
		_, err := fmt.Fprintf(w, "No in_progress claim on %s; nothing to recover\n", result.Ref)
		return err
	}
	holder := result.PriorHolder
	if holder == nil {
		_, err := fmt.Fprintf(w, "Cleared in_progress claim on %s\n", result.Ref)
		return err
	}
	_, err := fmt.Fprintf(w, "Cleared in_progress claim on %s (was %s on host=%s pid=%d since %s)\n",
		result.Ref, holder.Op, holder.Host, holder.PID, holder.Started.Format("2006-01-02 15:04:05Z"))
	return err
}

func flattenPlanMoves(changes []response.ReconcileChange) []response.ReconcileMove {
	var moves []response.ReconcileMove
	for _, change := range changes {
		if change.Replaced != nil {
			moves = append(moves, response.ReconcileMove{From: change.Replaced.From, To: change.Replaced.To})
			moves = append(moves, change.Replaced.Companions...)
		}
		moves = append(moves, response.ReconcileMove{From: change.From, To: change.To})
		moves = append(moves, change.Companions...)
	}
	return moves
}
