package agentruntimemcp

import "github.com/modelcontextprotocol/go-sdk/mcp"

// Read-only labels
//
// AgentRuntime labels every published tool read-only or not, and records where the label came from. What a tool
// says in tools/list always wins:
//
//   - Annotations: ReadOnly()     the tool changes nothing (readOnlyHint: true).
//   - Annotations: Writes(true)   the tool may change things (destructiveHint: true; Writes(false) when it only
//     adds, never overwrites or deletes).
//   - no Annotations              AgentRuntime infers the label from the tool name and shows it as inferred. The
//     rule is narrow: a read verb (get, list, search, find, fetch, read, retrieve, describe, count, lookup) and
//     nothing that writes, runs caller input, spends money or returns secrets.
//
// Use Writes for a tool whose name reads like a read but is not one (a get that acknowledges a queue, a find that
// creates what it does not find), so it is never inferred read-only. Writes sets destructiveHint rather than
// readOnlyHint: false because go-sdk before v1.5 leaves readOnlyHint: false off the wire; MCP defines
// destructiveHint only for tools that are not read-only, so the platform reads it as an explicit "not read-only"
// on every go-sdk version.
//
// Control-service reads the hints when it discovers a server and on its 15-minute schema check, so a changed hint
// reaches the platform without a manual publish (control-service docs/mcp-tool-read-only-labels.md).

// ReadOnly returns annotations that mark a tool read-only: it creates, updates, deletes and sends nothing, starts
// no job, acknowledges nothing, mints no credential and stores no output.
func ReadOnly() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true}
}

// Writes returns annotations that mark a tool as one that may change things, so the platform never labels it
// read-only. destructive is false when the tool only adds (creates, sends) and never overwrites or deletes.
func Writes(destructive bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{DestructiveHint: &destructive}
}
