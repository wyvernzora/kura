package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	tapeapi "github.com/wyvernzora/kura/services/tape-backup/pkg/api"
)

// TapeStatus writes the read-only tape status response.
func TapeStatus(w io.Writer, result tapeapi.StatusResult, asJSON bool) error {
	if asJSON {
		return writeTapeJSON(w, result)
	}
	if err := writeDriveStatus(w, result.Drive); err != nil {
		return err
	}
	if err := writeLiveSessions(w, result.LiveSessions); err != nil {
		return err
	}
	if err := writeTapeRoster(w, result.Consult); err != nil {
		return err
	}
	return writeInitDrafts(w, result.InitDrafts)
}

func writeDriveStatus(w io.Writer, drive tapeapi.DriveStatus) error {
	if _, err := fmt.Fprintf(w, "Drive: %s", drive.State); err != nil {
		return err
	}
	if drive.Message != "" {
		if _, err := fmt.Fprintf(w, " — %s", drive.Message); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

func writeLiveSessions(w io.Writer, sessions []tapeapi.LiveSession) error {
	if len(sessions) == 0 {
		_, err := fmt.Fprintln(w, "Live sessions: none")
		return err
	}
	if _, err := fmt.Fprintln(w, "Live sessions:"); err != nil {
		return err
	}
	for _, session := range sessions {
		if _, err := fmt.Fprintf(
			w,
			"  %s: %d plan(s)\n",
			session.SessionID,
			len(session.Plans),
		); err != nil {
			return err
		}
	}
	return nil
}

func writeTapeRoster(w io.Writer, consult *tapeapi.ConsultResult) error {
	if consult == nil || len(consult.Drafts) == 0 {
		_, err := fmt.Fprintln(w, "Roster: none")
		return err
	}
	if _, err := fmt.Fprintln(w, "Roster:"); err != nil {
		return err
	}
	for _, plan := range consult.Drafts {
		if _, err := fmt.Fprintf(
			w,
			"  %s: %s — %s\n",
			plan.Target.TapeID,
			plan.PlanID,
			strings.Join(planSeries(plan), ", "),
		); err != nil {
			return err
		}
	}
	return nil
}

func writeInitDrafts(w io.Writer, drafts []tapeapi.InitPlanAwaitingApproval) error {
	if len(drafts) == 0 {
		_, err := fmt.Fprintln(w, "Init drafts awaiting approval: none")
		return err
	}
	if _, err := fmt.Fprintln(w, "Init drafts awaiting approval:"); err != nil {
		return err
	}
	for _, draft := range drafts {
		if _, err := fmt.Fprintf(
			w,
			"  %s for %s: %d series claimed, age %ds\n",
			draft.PlanID,
			draft.TapeID,
			len(draft.Claimed),
			draft.AgeSeconds,
		); err != nil {
			return err
		}
	}
	return nil
}

// TapeConsult writes the sizing line and assignment roster.
func TapeConsult(w io.Writer, result tapeapi.ConsultResult, asJSON bool) error {
	if asJSON {
		return writeTapeJSON(w, result)
	}
	sizing := result.Report.Sizing
	generation := sizing.NominalBlankMediaGeneration
	if generation == "" {
		generation = "tape"
	}
	if _, err := fmt.Fprintf(
		w,
		"Roster covers %s of %s pending; bring ~%d blank %s.\n",
		formatBytes(sizing.RosterBytes),
		formatBytes(sizing.PendingBytes),
		sizing.BringBlanks,
		generation,
	); err != nil {
		return err
	}
	if len(result.Report.Assignments) == 0 {
		_, err := fmt.Fprintln(w, "Roster: none")
		return err
	}
	if _, err := fmt.Fprintln(w, "Roster:"); err != nil {
		return err
	}
	for _, assignment := range result.Report.Assignments {
		series := make([]string, 0, len(assignment.Series))
		for _, item := range assignment.Series {
			series = append(series, item.MetadataRef)
		}
		if _, err := fmt.Fprintf(
			w,
			"  %s: %s (%s)\n",
			assignment.Target.TapeID,
			strings.Join(series, ", "),
			formatBytes(assignment.Bytes),
		); err != nil {
			return err
		}
	}
	return nil
}

// TapePlan writes a targeted plan preview and its ceremony requirement.
func TapePlan(w io.Writer, result tapeapi.PlanResult, asJSON bool) error {
	if asJSON {
		return writeTapeJSON(w, result)
	}
	if result.Plan.PlanID == "" {
		_, err := fmt.Fprintf(w, "Classification: %s\nNo pending series for this tape.\n", result.Classification)
		return err
	}
	if err := writeTapePlan(w, result.Classification, result.Plan); err != nil {
		return err
	}
	if result.Classification == "init" {
		if _, err := fmt.Fprintf(
			w,
			"Media: %s in use, %s capacity.\n",
			formatBytes(result.Target.UsedBytes),
			formatBytes(result.Target.CapacityBytes),
		); err != nil {
			return err
		}
		identity := "no readable identity"
		if result.Target.VolumeID != "" {
			identity = "observed volume " + result.Target.VolumeID
		}
		if _, err := fmt.Fprintf(
			w,
			"Attestation: %s, serial %s; init will format this cartridge.\n",
			identity,
			result.Plan.Target.MediumSerial,
		); err != nil {
			return err
		}
		_, err := fmt.Fprintf(
			w,
			"Approval: required — run `kura tape approve %s`.\n",
			result.Plan.PlanID,
		)
		return err
	}
	_, err := fmt.Fprintln(w, "Approval: not required; fill preview was not persisted.")
	return err
}

// TapeRun writes the exact plan and series selected by the service.
func TapeRun(w io.Writer, result tapeapi.RunResult, asJSON bool) error {
	if asJSON {
		return writeTapeJSON(w, result)
	}
	if _, err := fmt.Fprintf(
		w,
		"Executed %s plan %s on %s.\n",
		result.Classification,
		result.Plan.PlanID,
		result.Plan.Target.TapeID,
	); err != nil {
		return err
	}
	return writePlanSeries(w, result.Plan)
}

func writeTapePlan(w io.Writer, classification string, plan tapeapi.Plan) error {
	if _, err := fmt.Fprintf(
		w,
		"Classification: %s\nPlan: %s\nTape: %s\nActions:\n",
		classification,
		plan.PlanID,
		plan.Target.TapeID,
	); err != nil {
		return err
	}
	for _, action := range plan.Actions {
		if _, err := fmt.Fprintf(w, "  %s%s\n", action.Type, actionSummary(action)); err != nil {
			return err
		}
	}
	return writePlanSeries(w, plan)
}

func writePlanSeries(w io.Writer, plan tapeapi.Plan) error {
	series := planSeries(plan)
	if len(series) == 0 {
		_, err := fmt.Fprintln(w, "Series: none")
		return err
	}
	if _, err := fmt.Fprintln(w, "Series:"); err != nil {
		return err
	}
	for _, ref := range series {
		if _, err := fmt.Fprintf(w, "  %s\n", ref); err != nil {
			return err
		}
	}
	return nil
}

func planSeries(plan tapeapi.Plan) []string {
	series := make([]string, 0)
	for _, action := range plan.Actions {
		if action.Type == "backup" && action.MetadataRef != nil {
			series = append(series, *action.MetadataRef)
		}
	}
	return series
}

func actionSummary(action tapeapi.Action) string {
	switch action.Type {
	case "backup":
		if action.MetadataRef == nil || action.Generation == nil {
			return ""
		}
		return fmt.Sprintf(" %s generation=%d", *action.MetadataRef, *action.Generation)
	case "admit", "assert_volume":
		if action.VolumeID != nil {
			return " volume=" + *action.VolumeID
		}
	case "assert_inventory":
		if action.Snapshots != nil {
			return " snapshots=[" + strings.Join(*action.Snapshots, ", ") + "]"
		}
	}
	return ""
}

func writeTapeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
