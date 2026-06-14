package gnmi

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	gpath "github.com/openconfig/gnmi/path"
	gnmiproto "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/gnmi/value"
	gapi "github.com/openconfig/gnmic/pkg/api"
	"github.com/openconfig/gnmic/pkg/api/target"
)

// subscriptionName is the gnmic-side name used for this session's single
// subscription. It is reused across re-Subscribe calls (the auto-fallback
// ladder) so StopSubscription can clear the prior subscription's state.
const subscriptionName = "default"

// GnmicDialer implements Dialer using the gnmic library.
type GnmicDialer struct{}

// Dial creates a gnmic-backed Session connected to the given target.
func (d *GnmicDialer) Dial(ctx context.Context, spec TargetSpec) (Session, error) {
	opts := []gapi.TargetOption{
		gapi.Name("gnmi-discovery"),
		gapi.Address(spec.Host),
	}
	if spec.Username != "" {
		opts = append(opts, gapi.Username(spec.Username))
	}
	if spec.Password != "" {
		opts = append(opts, gapi.Password(spec.Password))
	}

	// TLS is the default (secure by default): explicit CA/cert/key supply
	// verification/mTLS material; skip_verify keeps TLS but does not verify the
	// target cert (honored INDEPENDENTLY of that material, e.g. mTLS against a
	// self-signed device cert). Plaintext requires an EXPLICIT insecure opt-in.
	// With none of these set, gnmic establishes TLS using the system root CAs.
	if spec.CAFile != "" {
		opts = append(opts, gapi.TLSCA(spec.CAFile))
	}
	if spec.CertFile != "" {
		opts = append(opts, gapi.TLSCert(spec.CertFile))
	}
	if spec.KeyFile != "" {
		opts = append(opts, gapi.TLSKey(spec.KeyFile))
	}
	if spec.SkipVerify {
		opts = append(opts, gapi.SkipVerify(true))
	}
	if spec.Insecure {
		opts = append(opts, gapi.Insecure(true))
	}

	tg, err := gapi.NewTarget(opts...)
	if err != nil {
		return nil, fmt.Errorf("gnmi dial: create target: %w", err)
	}
	if err := tg.CreateGNMIClient(ctx); err != nil {
		return nil, fmt.Errorf("gnmi dial: create client: %w", err)
	}
	return &gnmicSession{tg: tg}, nil
}

// gnmicSession wraps a gnmic Target and implements Session.
type gnmicSession struct {
	tg *target.Target
	// subCancel cancels the context driving the active SubscribeChan producer
	// goroutine. It is set by Subscribe and invoked by Close so the producer
	// always observes cancellation, even while it is blocked in gnmic's
	// internal retry-timer wait (which only selects on this context).
	subCancel context.CancelFunc
	// encoding is the request encoding negotiated from the target's advertised
	// Capabilities (set by Capabilities()); empty until then, defaulting to
	// json_ietf via enc().
	encoding string
}

// enc returns the negotiated request encoding, defaulting to json_ietf when
// Capabilities has not run or advertised nothing usable.
func (s *gnmicSession) enc() string {
	if s.encoding != "" {
		return s.encoding
	}
	return "json_ietf"
}

// negotiateEncoding picks the request encoding from the target's advertised
// Capabilities encodings: prefer JSON_IETF (OpenConfig's canonical encoding),
// fall back to JSON (e.g. NX-OS advertises JSON only), else default to json_ietf
// as a best effort. decodeTypedValue handles both JSON_IETF and JSON responses,
// so either negotiated value yields the same decoded shape downstream.
func negotiateEncoding(advertised []string) string {
	hasJSON := false
	for _, e := range advertised {
		switch strings.ToUpper(strings.TrimSpace(e)) {
		case "JSON_IETF":
			return "json_ietf"
		case "JSON":
			hasJSON = true
		}
	}
	if hasJSON {
		return "json"
	}
	return "json_ietf"
}

// Capabilities runs the gNMI Capabilities RPC and returns a normalized result.
func (s *gnmicSession) Capabilities(ctx context.Context) (*CapabilitiesResult, error) {
	resp, err := s.tg.Capabilities(ctx)
	if err != nil {
		return nil, fmt.Errorf("gnmi capabilities: %w", err)
	}
	result := mapCapabilities(resp)
	// Negotiate the request encoding from what the target advertises so a
	// JSON-only target (e.g. NX-OS) isn't sent a JSON_IETF request it rejects.
	s.encoding = negotiateEncoding(result.Encodings)
	return result, nil
}

// Subscribe opens a gNMI STREAM subscription.
//
// We use tg.SubscribeChan (not SubscribeStreamChan): it returns buffered
// (cap-1) channels of *target.SubscribeResponse / *target.TargetError, and its
// producer goroutine's sends and retry-timer wait all select on the context we
// pass, so the producer exits cleanly once that context is cancelled. We derive
// that context from the caller's ctx and store its cancel on the session so
// Close() can stop the producer (and its gRPC connection) even when it is
// blocked mid-retry. This avoids the goroutine/connection leak that
// SubscribeStreamChan caused on reconnect (its producer looped forever on a
// bare `goto SUBSC` and only watched the parent ctx).
//
// Callers MUST call Session.Close() when the stream ends; the runner satisfies
// this via `defer sess.Close()` in runOnce.
func (s *gnmicSession) Subscribe(ctx context.Context, mode Mode, paths []string, sampleIntervalMs int) (<-chan Notification, <-chan error, error) {
	// Tear down any prior subscription on this session FIRST — before building or
	// validating the new request — so a build error can never leak the previous
	// producer goroutine + gRPC stream. The auto-fallback ladder in the runner
	// calls Subscribe twice on the same session (on_change, then sample on
	// downgrade); cancelling subCancel is the only thing that unblocks a producer
	// parked in gnmic's retry-timer wait. Cancel funcs are idempotent, so a later
	// Close() calling subCancel again is harmless. StopSubscription is a no-op for
	// an unknown name, so it is safe before any prior subscribe.
	if s.subCancel != nil {
		s.subCancel()
	}
	s.tg.StopSubscription(subscriptionName)

	subOpts := []gapi.GNMIOption{
		gapi.SubscriptionListModeSTREAM(),
		gapi.Encoding(s.enc()),
	}

	for _, p := range paths {
		var pathOpts []gapi.GNMIOption
		pathOpts = append(pathOpts, gapi.Path(p))
		switch mode {
		case OnChange:
			pathOpts = append(pathOpts, gapi.SubscriptionModeON_CHANGE())
		default: // Sample
			pathOpts = append(pathOpts, gapi.SubscriptionModeSAMPLE())
			if sampleIntervalMs > 0 {
				pathOpts = append(pathOpts, gapi.SampleInterval(time.Duration(sampleIntervalMs)*time.Millisecond))
			}
		}
		subOpts = append(subOpts, gapi.Subscription(pathOpts...))
	}

	req, err := gapi.NewSubscribeRequest(subOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("gnmi subscribe: build request: %w", err)
	}

	// Own context for the producer so Close() can stop it independently of the
	// caller's ctx lifetime.
	subCtx, cancel := context.WithCancel(ctx)
	s.subCancel = cancel

	rawResp, rawErr := s.tg.SubscribeChan(subCtx, req, subscriptionName)

	notes := make(chan Notification)
	errs := make(chan error, 1)

	go func() {
		defer close(notes)
		defer close(errs)
		for {
			select {
			case <-subCtx.Done():
				return
			case wrapped, ok := <-rawResp:
				if !ok {
					// rawResp closed — but SubscribeChan may have already queued an
					// error on rawErr that this select didn't pick (the async
					// ON_CHANGE-rejection path auto mode depends on). Drain it
					// non-blocking and forward it; otherwise streamLoop sees a clean
					// notes close, returns nil, and the target reconnects at on_change
					// forever instead of downgrading to SAMPLE/GET.
					select {
					case terr, ok := <-rawErr:
						if ok && terr != nil && terr.Err != nil {
							select {
							case errs <- terr.Err:
							case <-subCtx.Done():
							}
						}
					default:
					}
					return
				}
				// SubscribeChan wraps the proto response in .Response.
				resp := wrapped.Response
				if resp == nil {
					continue
				}
				if resp.GetSyncResponse() {
					select {
					case notes <- Notification{SyncDone: true}:
					case <-subCtx.Done():
						return
					}
					continue
				}
				if upd := resp.GetUpdate(); upd != nil {
					n := convertNotification(upd)
					select {
					case notes <- n:
					case <-subCtx.Done():
						return
					}
				}
			case terr, ok := <-rawErr:
				if !ok {
					return
				}
				// TargetError wraps the underlying error in .Err.
				if terr != nil && terr.Err != nil {
					select {
					case errs <- terr.Err:
					case <-subCtx.Done():
					}
				}
				return
			}
		}
	}()

	return notes, errs, nil
}

// GetOnce performs a single gNMI Get over the given paths.
func (s *gnmicSession) GetOnce(ctx context.Context, paths []string) (Notification, error) {
	// Fast path: one Get for all paths — most targets handle a multi-path Get fine.
	if n, err := s.getPaths(ctx, paths); err == nil {
		return n, nil
	}
	// A multi-path Get can fail ATOMICALLY when the target returns
	// NotFound/Unimplemented for one optional subtree it doesn't model (e.g.
	// switched-vlan or network-instance VLAN/VRF leaves). Retry per path and
	// tolerate the per-path failures so one unsupported optional path doesn't
	// abort the whole discovery pass (dropping otherwise-available hostname/
	// interface data and leaving the target reconnecting with no ingest). Only
	// surface an error when EVERY path fails (a genuine transport/auth problem).
	var result Notification
	result.SyncDone = true
	got := 0
	var lastErr error
	for _, p := range paths {
		n, err := s.getPaths(ctx, []string{p})
		if err != nil {
			lastErr = err
			continue
		}
		result.Updates = append(result.Updates, n.Updates...)
		result.Deletes = append(result.Deletes, n.Deletes...)
		got++
	}
	if got == 0 && lastErr != nil {
		return Notification{}, fmt.Errorf("gnmi get: all paths failed: %w", lastErr)
	}
	return result, nil
}

// getPaths issues a single gNMI Get for the given paths and merges the response
// notifications into one Notification.
func (s *gnmicSession) getPaths(ctx context.Context, paths []string) (Notification, error) {
	getOpts := []gapi.GNMIOption{
		gapi.Encoding(s.enc()),
		gapi.DataTypeALL(),
	}
	for _, p := range paths {
		getOpts = append(getOpts, gapi.Path(p))
	}
	req, err := gapi.NewGetRequest(getOpts...)
	if err != nil {
		return Notification{}, fmt.Errorf("gnmi get: build request: %w", err)
	}

	resp, err := s.tg.Get(ctx, req)
	if err != nil {
		return Notification{}, fmt.Errorf("gnmi get: %w", err)
	}

	var result Notification
	result.SyncDone = true
	for _, notif := range resp.GetNotification() {
		n := convertNotification(notif)
		result.Updates = append(result.Updates, n.Updates...)
		result.Deletes = append(result.Deletes, n.Deletes...)
	}
	return result, nil
}

// Close releases the underlying gNMI connection. It first cancels the
// subscribe context so the gnmic producer goroutine exits (even if blocked in
// its internal retry-timer wait), then closes the target's gRPC connection.
func (s *gnmicSession) Close() error {
	if s.subCancel != nil {
		s.subCancel()
	}
	return s.tg.Close()
}

// vendorCanonical maps a lower-cased vendor token (as it may appear within a
// SupportedModel Organization string) to the clean display name surfaced as the
// discovered vendor. NVIDIA Cumulus may report "NVIDIA", "Cumulus", or
// "Mellanox" depending on release; all three are recognized so the derived
// vendor still lines up with the nvidia_cumulus overlay's aliases.
var vendorCanonical = map[string]string{
	"arista":   "Arista",
	"nokia":    "Nokia",
	"cisco":    "Cisco",
	"juniper":  "Juniper",
	"nvidia":   "NVIDIA",
	"cumulus":  "Cumulus",
	"mellanox": "Mellanox",
	"huawei":   "Huawei",
	"dell":     "Dell",
}

// vendorTokenOrder fixes the scan order over vendorCanonical so the first match
// is deterministic across runs (map iteration order is randomized).
var vendorTokenOrder = []string{"arista", "nokia", "cisco", "juniper", "nvidia", "cumulus", "mellanox", "huawei", "dell"}

// nosCanonical maps a network-OS token (as it may appear in a SupportedModel
// Organization) to its canonical name. A NOS is software that runs on hardware
// from a separate OEM, so it is detected independently of vendorCanonical and
// never becomes a device Manufacturer — it only biases profile selection.
// SONiC is the case in point: a Dell/Edgecore/etc. box runs SONiC, so the
// manufacturer stays the hardware OEM while the profile is the sonic overlay.
var nosCanonical = map[string]string{
	"sonic": "SONiC",
}

// nosTokenOrder fixes the NOS scan order (deterministic; map order is randomized).
var nosTokenOrder = []string{"sonic"}

// mapCapabilities converts a raw gNMI CapabilityResponse to our CapabilitiesResult.
func mapCapabilities(resp *gnmiproto.CapabilityResponse) *CapabilitiesResult {
	result := &CapabilitiesResult{}

	models := resp.GetSupportedModels()
	// Scan all SupportedModel Organizations for a known hardware-vendor token.
	// We collect the best (lowest index in vendorTokenOrder) match across all
	// models so a higher-priority token wins regardless of which model appears
	// first in the list. If nothing matches, Vendor stays "" — we deliberately do
	// NOT fall back to models[0]'s raw Organization, which would surface noise
	// like "OpenConfig working group" as a literal NetBox manufacturer. The
	// profile Store.Match still works because each canonical token is a substring
	// of itself (and of the overlay aliases).
	bestIdx := len(vendorTokenOrder) // sentinel: no match yet
	for _, m := range models {
		org := strings.ToLower(m.GetOrganization())
		for idx, tok := range vendorTokenOrder {
			if idx >= bestIdx {
				break // no improvement possible
			}
			if strings.Contains(org, tok) {
				bestIdx = idx
				result.Vendor = vendorCanonical[tok]
				break
			}
		}
	}
	// Network-OS detection is independent of the hardware vendor: a Dell-built
	// SONiC box matches both "dell" (Vendor/manufacturer) and "sonic" (NOS, which
	// biases profile selection). Same lowest-index-wins scan over nosTokenOrder.
	nosIdx := len(nosTokenOrder)
	for _, m := range models {
		org := strings.ToLower(m.GetOrganization())
		for idx, tok := range nosTokenOrder {
			if idx >= nosIdx {
				break
			}
			if strings.Contains(org, tok) {
				nosIdx = idx
				result.NOS = nosCanonical[tok]
				break
			}
		}
	}
	for _, m := range models {
		result.Models = append(result.Models, m.GetName())
	}

	for _, enc := range resp.GetSupportedEncodings() {
		result.Encodings = append(result.Encodings, enc.String())
	}

	return result
}

// convertNotification maps a proto *gnmi.Notification to our Notification.
func convertNotification(n *gnmiproto.Notification) Notification {
	if n == nil {
		return Notification{}
	}
	prefix := pathToString(n.GetPrefix())

	result := Notification{}
	for _, upd := range n.GetUpdate() {
		p := joinPaths(prefix, pathToString(upd.GetPath()))
		result.Updates = append(result.Updates, Update{
			Path:  p,
			Value: decodeTypedValue(upd.GetVal()),
		})
	}
	for _, del := range n.GetDelete() {
		result.Deletes = append(result.Deletes, joinPaths(prefix, pathToString(del)))
	}
	return result
}

// pathToString renders a *gnmi.Path to an absolute XPath-style string.
// Keys within each element are sorted for deterministic output.
//
// The Path.Origin field is intentionally not rendered. Profile paths are
// origin-less OpenConfig xpaths, so omitting origin lets incoming updates
// match the profile regardless of whether the target sets origin (e.g.
// "openconfig"). Prepending origin would break AllowsPath / profile matching.
func pathToString(p *gnmiproto.Path) string {
	if p == nil {
		return ""
	}
	var b strings.Builder
	for _, elem := range p.GetElem() {
		b.WriteByte('/')
		b.WriteString(elem.GetName())
		if len(elem.GetKey()) > 0 {
			keys := make([]string, 0, len(elem.GetKey()))
			for k := range elem.GetKey() {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(&b, "[%s=%s]", k, elem.GetKey()[k])
			}
		}
	}
	// Fall back to the deprecated repeated Path.element when Path.elem is absent —
	// older targets/proxies still populate it, and rendering empty here would make
	// AllowsPath drop every update. gpath.ToStrings reads the deprecated field
	// internally (so we never reference it directly); each entry is an
	// already-rendered element (e.g. "interface[name=eth0]"). prefix=false keeps
	// origin/target out, consistent with the elem rendering above.
	if len(p.GetElem()) == 0 {
		for _, e := range gpath.ToStrings(p, false) {
			b.WriteByte('/')
			b.WriteString(e)
		}
	}
	return b.String()
}

// joinPaths concatenates a prefix path and a leaf path, avoiding double slashes.
func joinPaths(prefix, path string) string {
	if prefix == "" {
		return path
	}
	if path == "" {
		return prefix
	}
	return strings.TrimSuffix(prefix, "/") + "/" + strings.TrimPrefix(path, "/")
}

// decodeTypedValue converts a *gnmi.TypedValue to a plain Go value.
func decodeTypedValue(tv *gnmiproto.TypedValue) any {
	if tv == nil {
		return nil
	}
	switch v := tv.GetValue().(type) {
	case *gnmiproto.TypedValue_StringVal:
		return v.StringVal
	case *gnmiproto.TypedValue_IntVal:
		return v.IntVal
	case *gnmiproto.TypedValue_UintVal:
		return v.UintVal
	case *gnmiproto.TypedValue_BoolVal:
		return v.BoolVal
	case *gnmiproto.TypedValue_DoubleVal:
		return v.DoubleVal
	case *gnmiproto.TypedValue_BytesVal:
		return v.BytesVal
	case *gnmiproto.TypedValue_AsciiVal:
		return v.AsciiVal
	case *gnmiproto.TypedValue_JsonIetfVal:
		var decoded any
		if err := json.Unmarshal(v.JsonIetfVal, &decoded); err == nil {
			return decoded
		}
		return string(v.JsonIetfVal)
	case *gnmiproto.TypedValue_JsonVal:
		var decoded any
		if err := json.Unmarshal(v.JsonVal, &decoded); err == nil {
			return decoded
		}
		return string(v.JsonVal)
	case *gnmiproto.TypedValue_LeaflistVal:
		// A native leaf-list (e.g. trunk-vlans when a target ignores the json_ietf
		// encoding hint): decode each element to a plain Go value, yielding []any —
		// the same shape JSON_IETF produces, so downstream leaf-list consumers
		// (e.g. mapping.expandTrunkVlans) handle both encodings uniformly.
		if v.LeaflistVal == nil {
			return nil
		}
		out := make([]any, 0, len(v.LeaflistVal.GetElement()))
		for _, el := range v.LeaflistVal.GetElement() {
			out = append(out, decodeTypedValue(el))
		}
		return out
	default:
		// Remaining scalar types — including the deprecated FloatVal/DecimalVal that
		// older targets may still send — are decoded via the openconfig value
		// helper, so we never reference the deprecated proto fields directly. Falls
		// back to the proto string repr only for a genuinely unknown type.
		if s, err := value.ToScalar(tv); err == nil {
			return s
		}
		return tv.String()
	}
}
