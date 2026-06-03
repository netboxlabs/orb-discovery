package gnmi

import "context"

// Update is a single normalized leaf update from a gNMI notification.
// Path is the absolute OpenConfig path; Value is the decoded scalar/string.
type Update struct {
	Path  string
	Value any
}

// Notification is our transport-agnostic view of a gNMI SubscribeResponse,
// Get response, or sampled snapshot.
type Notification struct {
	Updates  []Update
	Deletes  []string // absolute paths removed
	SyncDone bool     // true on the sync_response boundary / end of a Get
}

// CapabilitiesResult is the subset of a gNMI Capabilities response we use.
// Note: gNMI Capabilities does not reliably advertise ON_CHANGE support per
// path, so we do not model it here — the runner attempts ON_CHANGE and falls
// back on the Subscribe rejection instead.
type CapabilitiesResult struct {
	Vendor    string
	Models    []string
	Encodings []string
}

// Mode is a delivery mode.
type Mode string

const (
	OnChange Mode = "on_change"
	Sample   Mode = "sample"
	Get      Mode = "get"
)

// Session is one connection to a gNMI target.
type Session interface {
	// Capabilities runs the gNMI Capabilities RPC.
	Capabilities(ctx context.Context) (*CapabilitiesResult, error)
	// Subscribe opens a stream for mode (OnChange or Sample) over paths and
	// returns a notifications channel and an errors channel. The channels
	// close when ctx is cancelled or the stream ends.
	Subscribe(ctx context.Context, mode Mode, paths []string, sampleIntervalMs int) (<-chan Notification, <-chan error, error)
	// GetOnce performs a single gNMI Get over paths.
	GetOnce(ctx context.Context, paths []string) (Notification, error)
	// Close releases the connection.
	Close() error
}

// Dialer builds Sessions from a target spec.
type Dialer interface {
	Dial(ctx context.Context, target TargetSpec) (Session, error)
}

// TargetSpec is the minimal connection info the dialer needs.
type TargetSpec struct {
	Host       string
	Username   string
	Password   string
	SkipVerify bool
	CAFile     string
	CertFile   string
	KeyFile    string
}
