package flow

// FlowRecord is a normalized, sampling-rate-adjusted flow record.
// It is produced by the listener from a goflow2 ProtoProducerMessage.
type FlowRecord struct {
	SamplerAddr string
	SrcAddr     string
	DstAddr     string
	SrcPort     uint32
	DstPort     uint32
	// Proto is the IP protocol number (6=TCP, 17=UDP, 1=ICMP, …).
	Proto   uint32
	Bytes   uint64
	Packets uint64
	InIf    uint32
	OutIf   uint32
	SrcAs   uint32
	DstAs   uint32
}
