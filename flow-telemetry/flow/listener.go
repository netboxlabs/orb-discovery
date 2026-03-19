package flow

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"

	protoproducer "github.com/netsampler/goflow2/v2/producer/proto"
	"github.com/netsampler/goflow2/v2/utils"

	"github.com/netboxlabs/orb-discovery/flow-telemetry/config"
)

// channelFormat implements format.FormatInterface.
// It converts each ProtoProducerMessage to a FlowRecord and forwards it to a channel.
type channelFormat struct {
	ch chan<- FlowRecord
}

func (f *channelFormat) Format(data interface{}) ([]byte, []byte, error) {
	msg, ok := data.(*protoproducer.ProtoProducerMessage)
	if !ok {
		return nil, nil, nil
	}
	rec := fromProtoMessage(msg)
	select {
	case f.ch <- rec:
	default: // drop if channel full
	}
	return nil, nil, nil
}

// noopTransport implements transport.TransportInterface with a no-op Send.
type noopTransport struct{}

func (t *noopTransport) Send(_, _ []byte) error { return nil }

// fromProtoMessage converts a goflow2 protobuf flow message to a FlowRecord.
// Bytes and Packets are multiplied by the sampling rate when it is > 1.
func fromProtoMessage(msg *protoproducer.ProtoProducerMessage) FlowRecord {
	toIP := func(b []byte) string {
		if len(b) == 0 {
			return ""
		}
		return net.IP(b).String()
	}

	bytes := msg.Bytes
	packets := msg.Packets
	if msg.SamplingRate > 1 {
		bytes *= msg.SamplingRate
		packets *= msg.SamplingRate
	}

	return FlowRecord{
		SamplerAddr: toIP(msg.SamplerAddress),
		SrcAddr:     toIP(msg.SrcAddr),
		DstAddr:     toIP(msg.DstAddr),
		SrcPort:     msg.SrcPort,
		DstPort:     msg.DstPort,
		Proto:       msg.Proto,
		Bytes:       bytes,
		Packets:     packets,
		InIf:        msg.InIf,
		OutIf:       msg.OutIf,
		SrcAs:       msg.SrcAs,
		DstAs:       msg.DstAs,
	}
}

// NewListener starts a goflow2 UDP listener for the given policy config.
// host is the IP address to bind to; defaults to "0.0.0.0" when empty.
// Decoded FlowRecords are sent to the returned channel.
// The channel is closed when ctx is cancelled.
func NewListener(ctx context.Context, logger *slog.Logger, cfg config.PolicyConfig, host string, port int) (<-chan FlowRecord, error) {
	if host == "" {
		host = "0.0.0.0"
	}
	ch := make(chan FlowRecord, 10000)

	flowProducer, err := protoproducer.CreateProtoProducer(nil, protoproducer.CreateSamplingSystem)
	if err != nil {
		return nil, fmt.Errorf("creating flow producer: %w", err)
	}

	pipeConfig := &utils.PipeConfig{
		Format:    &channelFormat{ch: ch},
		Transport: &noopTransport{},
		Producer:  flowProducer,
	}

	workers := cfg.Workers
	if workers <= 0 {
		workers = 2
	}
	queueSize := cfg.QueueSize
	if queueSize <= 0 {
		queueSize = 10000
	}

	recv, err := utils.NewUDPReceiver(&utils.UDPReceiverConfig{
		Sockets:   workers,
		Workers:   workers,
		QueueSize: queueSize,
	})
	if err != nil {
		return nil, fmt.Errorf("creating UDP receiver: %w", err)
	}

	pipe := selectPipe(strings.ToLower(cfg.Protocol), pipeConfig)

	if err := recv.Start(host, port, pipe.DecodeFlow); err != nil {
		return nil, fmt.Errorf("starting UDP receiver on %s:%d: %w", host, port, err)
	}

	logger.Info("flow listener started", "host", host, "port", port, "protocol", cfg.Protocol, "workers", workers)

	go func() {
		for {
			select {
			case <-ctx.Done():
				if err := recv.Stop(); err != nil {
					logger.Warn("error stopping flow receiver", "error", err)
				}
				close(ch)
				return
			case err, ok := <-recv.Errors():
				if !ok {
					return
				}
				if err != nil {
					logger.Warn("flow receiver error", "error", err)
				}
			}
		}
	}()

	return ch, nil
}

func selectPipe(protocol string, cfg *utils.PipeConfig) utils.FlowPipe {
	switch protocol {
	case "sflow":
		return utils.NewSFlowPipe(cfg)
	case "netflow5", "netflow9", "ipfix":
		return utils.NewNetFlowPipe(cfg)
	default: // "auto" or empty
		return utils.NewFlowPipe(cfg)
	}
}
