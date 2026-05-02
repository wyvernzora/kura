// Package jobs provides workflow-layer job machinery shared by all
// surfaces (CLI, MCP, REST). Long workflows return *Job[T]; callers
// either Wait for the typed result (CLI, sync MCP tools) or hand the
// job's ID off to a client that polls (long MCP tools, async REST
// handlers).
//
// Two job flavours:
//
//   - Pre-resolved (jobs.Resolved / jobs.Failed): terminal at
//     construction; not registered with the Registry; ID() == "" and
//     IsTracked() == false. Used by short workflows that complete
//     synchronously.
//   - Tracked (jobs.Submit): registered with the Registry; runs in a
//     goroutine; has a non-empty ID; IsTracked() == true. Used by
//     long workflows.
//
// Caller pattern is uniform: call j.Wait(ctx) for the result, or read
// j.ID() and friends to construct a JobHandle for handoff. The choice
// is per-surface, not per-call.
package jobs
