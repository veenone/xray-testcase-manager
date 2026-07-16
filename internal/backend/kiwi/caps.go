package kiwi

import "context"

// methodExists probes whether a Kiwi RPC method is registered by invoking it
// with sampleParams and inspecting the result. This is the mechanism used
// for plugin detection (spec §4.3: e.g. Requirement.filter({}) for the
// requirements plugin, ReviewRequest.filter({}) for the review plugin).
//
// This task (P4.1) builds and tests only the mechanism; wiring its result
// into Capabilities() fields (SupportsRequirementObjects,
// SupportsIssueLinkTypes, SupportsReview) happens in P4.3 once the plugin
// read methods it gates actually exist.
//
// Returns:
//   - (true, nil)  — the call succeeded: the method is registered.
//   - (false, nil) — the server returned JSON-RPC code -32601 ("method not
//     found"): the plugin/method is absent.
//   - (true, err)  — any other error (e.g. PermissionDenied/ValueError from
//     modernrpc). Per spec §4.3 this means the method IS registered but the
//     call failed for another reason ("installed but degraded, token lacks
//     permission") — callers must not treat this as "absent"; err should be
//     surfaced as a diagnostic rather than silently swallowed.
func methodExists(ctx context.Context, c *Client, method string, sampleParams []any) (bool, error) {
	err := c.call(ctx, method, sampleParams, nil)
	if err == nil {
		return true, nil
	}
	if isMethodNotFound(err) {
		return false, nil
	}
	return true, err
}
