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
// otherwise emits a one-line summary.
func ApplyReconcile(w io.Writer, result response.ReconcileApply, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	_, err := fmt.Fprintf(w, "Applied %d reconcile moves for %s\n", result.AppliedMoves, result.Series)
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
