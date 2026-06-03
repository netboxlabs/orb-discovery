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

	// TLS logic: explicit CA/cert/key wins; then SkipVerify; else Insecure (plaintext).
	if spec.CAFile != "" || spec.CertFile != "" || spec.KeyFile != "" {
		if spec.CAFile != "" {
			opts = append(opts, gapi.TLSCA(spec.CAFile))
		}
		if spec.CertFile != "" {
			opts = append(opts, gapi.TLSCert(spec.CertFile))
		}
		if spec.KeyFile != "" {
			opts = append(opts, gapi.TLSKey(spec.KeyFile))
		}
	} else if spec.SkipVerify {
		opts = append(opts, gapi.SkipVerify(true))
	} else {
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

	rawResp, rawErr := s.tg.SubscribeChan(subCtx, req, "default")

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

// mapCapabilities converts a raw gNMI CapabilityResponse to our CapabilitiesResult.
func mapCapabilities(resp *gnmiproto.CapabilityResponse) *CapabilitiesResult {
	result := &CapabilitiesResult{}

	models := resp.GetSupportedModels()
	// The first SupportedModel is frequently an OpenConfig model whose
	// Organization is "OpenConfig working group", not the hardware vendor.
	// Scan all models and prefer the first Organization that names a known
	// hardware vendor; fall back to models[0] so behavior is no worse than
	// taking the first organization blindly.
	vendorTokens := []string{"arista", "nokia", "cisco", "juniper", "nvidia", "huawei"}
	for _, m := range models {
		org := strings.ToLower(m.GetOrganization())
		matched := false
		for _, tok := range vendorTokens {
			if strings.Contains(org, tok) {
				result.Vendor = m.GetOrganization()
				matched = true
				break
			}
		}
		if matched {
			break
		}
	}
	if result.Vendor == "" && len(models) > 0 {
		result.Vendor = models[0].GetOrganization()
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
	default:
		return tv.String()
	}
}
