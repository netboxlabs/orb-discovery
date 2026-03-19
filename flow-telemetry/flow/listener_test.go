package flow

import (
	"testing"

	"github.com/stretchr/testify/assert"

	protoproducer "github.com/netsampler/goflow2/v2/producer/proto"
	"github.com/netsampler/goflow2/v2/utils"
)

// --- fromProtoMessage ---

func TestFromProtoMessage_BasicFields(t *testing.T) {
	msg := &protoproducer.ProtoProducerMessage{}
	msg.SrcAddr = []byte{10, 0, 0, 1}
	msg.DstAddr = []byte{10, 0, 0, 2}
	msg.SamplerAddress = []byte{192, 168, 1, 1}
	msg.SrcPort = 1234
	msg.DstPort = 80
	msg.Proto = 6
	msg.Bytes = 1000
	msg.Packets = 10
	msg.InIf = 3
	msg.OutIf = 4
	msg.SrcAs = 64512
	msg.DstAs = 65000
	msg.SamplingRate = 1

	rec := fromProtoMessage(msg)

	assert.Equal(t, "10.0.0.1", rec.SrcAddr)
	assert.Equal(t, "10.0.0.2", rec.DstAddr)
	assert.Equal(t, "192.168.1.1", rec.SamplerAddr)
	assert.Equal(t, uint32(1234), rec.SrcPort)
	assert.Equal(t, uint32(80), rec.DstPort)
	assert.Equal(t, uint32(6), rec.Proto)
	assert.Equal(t, uint64(1000), rec.Bytes)
	assert.Equal(t, uint64(10), rec.Packets)
	assert.Equal(t, uint32(3), rec.InIf)
	assert.Equal(t, uint32(4), rec.OutIf)
	assert.Equal(t, uint32(64512), rec.SrcAs)
	assert.Equal(t, uint32(65000), rec.DstAs)
}

func TestFromProtoMessage_SamplingRateApplied(t *testing.T) {
	msg := &protoproducer.ProtoProducerMessage{}
	msg.Bytes = 100
	msg.Packets = 5
	msg.SamplingRate = 10

	rec := fromProtoMessage(msg)

	assert.Equal(t, uint64(1000), rec.Bytes)
	assert.Equal(t, uint64(50), rec.Packets)
}

func TestFromProtoMessage_SamplingRateOne_NoAdjustment(t *testing.T) {
	msg := &protoproducer.ProtoProducerMessage{}
	msg.Bytes = 500
	msg.Packets = 20
	msg.SamplingRate = 1

	rec := fromProtoMessage(msg)

	assert.Equal(t, uint64(500), rec.Bytes)
	assert.Equal(t, uint64(20), rec.Packets)
}

func TestFromProtoMessage_SamplingRateZero_NoAdjustment(t *testing.T) {
	// SamplingRate == 0 should NOT multiply (condition is > 1).
	msg := &protoproducer.ProtoProducerMessage{}
	msg.Bytes = 200
	msg.Packets = 8
	msg.SamplingRate = 0

	rec := fromProtoMessage(msg)

	assert.Equal(t, uint64(200), rec.Bytes)
	assert.Equal(t, uint64(8), rec.Packets)
}

func TestFromProtoMessage_EmptyAddresses(t *testing.T) {
	msg := &protoproducer.ProtoProducerMessage{}

	rec := fromProtoMessage(msg)

	assert.Equal(t, "", rec.SrcAddr)
	assert.Equal(t, "", rec.DstAddr)
	assert.Equal(t, "", rec.SamplerAddr)
}

func TestFromProtoMessage_IPv4(t *testing.T) {
	msg := &protoproducer.ProtoProducerMessage{}
	msg.SrcAddr = []byte{172, 16, 0, 1}

	rec := fromProtoMessage(msg)

	assert.Equal(t, "172.16.0.1", rec.SrcAddr)
}

func TestFromProtoMessage_IPv6(t *testing.T) {
	msg := &protoproducer.ProtoProducerMessage{}
	// IPv6 loopback: ::1
	msg.SrcAddr = []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}

	rec := fromProtoMessage(msg)

	assert.Equal(t, "::1", rec.SrcAddr)
}

func TestFromProtoMessage_LargeSamplingRate(t *testing.T) {
	msg := &protoproducer.ProtoProducerMessage{}
	msg.Bytes = 1
	msg.Packets = 1
	msg.SamplingRate = 1000

	rec := fromProtoMessage(msg)

	assert.Equal(t, uint64(1000), rec.Bytes)
	assert.Equal(t, uint64(1000), rec.Packets)
}

// --- channelFormat ---

func TestChannelFormat_ValidMessage(t *testing.T) {
	ch := make(chan FlowRecord, 1)
	f := &channelFormat{ch: ch}

	msg := &protoproducer.ProtoProducerMessage{}
	msg.SrcAddr = []byte{1, 2, 3, 4}
	msg.Bytes = 42

	key, data, err := f.Format(msg)

	assert.NoError(t, err)
	assert.Nil(t, key)
	assert.Nil(t, data)
	assert.Len(t, ch, 1)

	rec := <-ch
	assert.Equal(t, "1.2.3.4", rec.SrcAddr)
	assert.Equal(t, uint64(42), rec.Bytes)
}

func TestChannelFormat_WrongType(t *testing.T) {
	ch := make(chan FlowRecord, 1)
	f := &channelFormat{ch: ch}

	key, data, err := f.Format("not a flow message")

	assert.NoError(t, err)
	assert.Nil(t, key)
	assert.Nil(t, data)
	assert.Empty(t, ch)
}

func TestChannelFormat_FullChannel_Drops(t *testing.T) {
	// Channel capacity = 0 (synchronous) — Format must not block.
	ch := make(chan FlowRecord, 0)
	f := &channelFormat{ch: ch}

	msg := &protoproducer.ProtoProducerMessage{}
	msg.Bytes = 99

	// Should not block or panic even though channel has no capacity.
	key, data, err := f.Format(msg)
	assert.NoError(t, err)
	assert.Nil(t, key)
	assert.Nil(t, data)
}

// --- noopTransport ---

func TestNoopTransport_Send(t *testing.T) {
	nt := &noopTransport{}
	assert.NoError(t, nt.Send([]byte("key"), []byte("data")))
	assert.NoError(t, nt.Send(nil, nil))
}

// --- selectPipe ---

func TestSelectPipe_Auto(t *testing.T) {
	cfg := &utils.PipeConfig{}
	pipe := selectPipe("auto", cfg)
	_, ok := pipe.(*utils.AutoFlowPipe)
	assert.True(t, ok, "expected *AutoFlowPipe for protocol 'auto'")
}

func TestSelectPipe_Empty_DefaultsToAuto(t *testing.T) {
	cfg := &utils.PipeConfig{}
	pipe := selectPipe("", cfg)
	_, ok := pipe.(*utils.AutoFlowPipe)
	assert.True(t, ok, "expected *AutoFlowPipe for empty protocol")
}

func TestSelectPipe_Unknown_DefaultsToAuto(t *testing.T) {
	cfg := &utils.PipeConfig{}
	pipe := selectPipe("cflow", cfg)
	_, ok := pipe.(*utils.AutoFlowPipe)
	assert.True(t, ok, "expected *AutoFlowPipe for unknown protocol")
}

func TestSelectPipe_SFlow(t *testing.T) {
	cfg := &utils.PipeConfig{}
	pipe := selectPipe("sflow", cfg)
	_, ok := pipe.(*utils.SFlowPipe)
	assert.True(t, ok, "expected *SFlowPipe for protocol 'sflow'")
}

func TestSelectPipe_NetFlowProtocols(t *testing.T) {
	cfg := &utils.PipeConfig{}
	for _, proto := range []string{"netflow5", "netflow9", "ipfix"} {
		pipe := selectPipe(proto, cfg)
		_, ok := pipe.(*utils.NetFlowPipe)
		assert.True(t, ok, "expected *NetFlowPipe for protocol %q", proto)
	}
}
