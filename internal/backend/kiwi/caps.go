package kiwi

import (
	"context"
	"errors"
)

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

// requirementsProbeMethod / reviewProbeMethod are the cheapest read RPCs
// each optional plugin exposes, probed once per TestConnection (spec §4.3,
// P4.3 brief item (a)).
//
//   - requirementsProbeMethod = "Requirement.filter" — cited to
//     tcms_requirements/rpc.py's @rpc_method(name="Requirement.filter")
//     (spec §4.3, §8.1).
//   - reviewProbeMethod = "ReviewRequest.filter" — spec §4.3 offers this OR
//     "ReviewRequest.filter_canonical" as the review-plugin probe; this
//     package picks "ReviewRequest.filter" as primary (it's also the name
//     TestMethodExistsProbe in client_test.go already exercises against this
//     exact method, cited to tcms_review/api.py in spec §8.2). The review
//     plugin's read/write surface itself is NOT implemented in this task —
//     see the "review" flag doc comment on Adapter for the deferred scope.
const (
	requirementsProbeMethod = "Requirement.filter"
	reviewProbeMethod       = "ReviewRequest.filter"
)

// detectPlugin probes method with an empty/harmless query and reports
// whether the plugin appears installed, per spec §4.3's rule refined by the
// P4.3 brief's extra guidance on transport failures:
//
//   - err == nil                     -> method is registered: true.
//   - isMethodNotFound(err)          -> -32601: plugin/method absent: false.
//   - err is a *kiwiRPCError (other) -> the request reached modernrpc and
//     the method IS registered server-side, but failed for another
//     application reason (e.g. PermissionDenied/ValueError) -- spec §4.3
//     calls this "installed, but the token lacks permission": treat as
//     installed-but-degraded (true). The capability this flips (e.g.
//     SupportsRequirementObjects) will surface the same degraded error to
//     the caller the moment ListRequirements is actually used, which is
//     this package's way of "not silently swallowing" the failure without
//     adding a second error-return path to TestConnection's signature.
//   - anything else (a raw transport/HTTP/decode failure, not a typed
//     *kiwiRPCError) -> carries NO signal about whether the method is
//     registered. The brief's detection guidance is explicit here: "treat a
//     transport error as unknown and default OFF" rather than methodExists'
//     own (true, err) fallback, which conflates transport failures with
//     confirmed-registered-but-degraded RPC errors. This is why detectPlugin
//     re-inspects the error type instead of using methodExists' bool return
//     directly.
func detectPlugin(ctx context.Context, c *Client, method string) bool {
	present, err := methodExists(ctx, c, method, []any{map[string]any{}})
	if err == nil {
		return present
	}
	var rpcErr *kiwiRPCError
	if errors.As(err, &rpcErr) {
		// methodExists already special-cased -32601 into (false, nil), so
		// any *kiwiRPCError reaching here is a non-32601 application error:
		// installed, degraded.
		return true
	}
	// Raw transport/HTTP/decode failure: unknown -> default OFF.
	return false
}
