package gnmi

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	gnmiproto "github.com/openconfig/gnmi/proto/gnmi"
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
}

// Capabilities runs the gNMI Capabilities RPC and returns a normalized result.
func (s *gnmicSession) Capabilities(ctx context.Context) (*CapabilitiesResult, error) {
	resp, err := s.tg.Capabilities(ctx)
	if err != nil {
		return nil, fmt.Errorf("gnmi capabilities: %w", err)
	}
	return mapCapabilities(resp), nil
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
		gapi.Encoding("json_ietf"),
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
	getOpts := []gapi.GNMIOption{
		gapi.Encoding("json_ietf"),
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
	case *gnmiproto.TypedValue_FloatVal: //nolint:staticcheck // deprecated proto field, kept for legacy target compat
		return float64(v.FloatVal) //nolint:staticcheck
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
	case *gnmiproto.TypedValue_DecimalVal: //nolint:staticcheck // deprecated proto field, kept for legacy target compat
		if v.DecimalVal != nil { //nolint:staticcheck
			return float64(v.DecimalVal.GetDigits()) / math.Pow10(int(v.DecimalVal.GetPrecision())) //nolint:staticcheck
		}
		return nil
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
		return tv.String()
	}
}
