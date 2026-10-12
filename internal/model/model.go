// Package model holds decision records and the actions the gate evaluates.
package model

const (
	KindKickoff = "kickoff"
	KindChange  = "change"

	StatusProposed   = "proposed"
	StatusReviewed   = "reviewed"
	StatusApproved   = "approved"
	StatusAuthorized = "authorized"
	StatusOverridden = "overridden"
	StatusRejected   = "rejected"
	StatusSuperseded = "superseded"

	VerdictAccept = "accept"
	VerdictRevise = "revise"
	VerdictBlock  = "block"
)

// GateOpenStatuses includes approved so decisions recorded before authorization
// was split still open the gate.
var GateOpenStatuses = map[string]bool{
	StatusApproved:   true,
	StatusAuthorized: true,
	StatusOverridden: true,
}

var OpenStatuses = map[string]bool{
	StatusProposed: true,
	StatusReviewed: true,
}

// ReviewResult is Jev's verdict. Valid defaults to true at the call site.
type ReviewResult struct {
	Verdict            string
	Summary            string
	Concerns           []string
	Valid              bool
	AffectedInvariant  string
	Conflict           string
	RequiredChange     string
	AcceptanceCriteria string
	Evidence           string
	Probabilities      map[string]any
	Code               string
}

// Action is one read, write, or shell attempt.
type Action struct {
	Kind     string
	Paths    []string
	Contents map[string]string
	Command  string
}

// GateResult is the allow or deny answer for an action.
type GateResult struct {
	Allow        bool
	UserMessage  string
	AgentMessage string
}

// Allow returns a permitted result.
func Allow() GateResult {
	return GateResult{Allow: true}
}

// IsGateOpen reports whether a decision authorizes work.
func IsGateOpen(status string) bool {
	return GateOpenStatuses[status]
}
