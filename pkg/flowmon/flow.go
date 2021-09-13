package flowmon

import (
	"encoding/binary"
	"fmt"
	"net"
	"reflect"

	flowmessage "github.com/netsampler/goflow2/pb"
)

// FlowDirection is an enum for Flow directions
type FlowDirection uint32

const (
	FlowDirectionIngress FlowDirection = 0x0
	FlowDirectionEgress  FlowDirection = 0x1
)

func (fd FlowDirection) String() string {
	switch fd {
	case FlowDirectionIngress:
		return "INGRESS"
	case FlowDirectionEgress:
		return "EGRESS"
	default:
		return "unknown"
	}
}

// Etype is an enum for EtherTypes
type Etype uint32

const (
	EtypeIPv4 Etype = 0x800
	EtypeARP  Etype = 0x806
	EtypeIPv6 Etype = 0x86DD
)

func (e Etype) String() string {
	switch e {
	case EtypeIPv4:
		return "IPv4"
	case EtypeIPv6:
		return "IPv6"
	case EtypeARP:
		return "ARP"
	default:
		return fmt.Sprintf("0x%x", int(e))
	}
}

// Proto is an enum for Network Protocols
type Proto uint32

const (
	ProtoICMP   Proto = 0x1
	ProtoTCP    Proto = 0x6
	ProtoUDP    Proto = 0x11
	ProtoICMPv6 Proto = 0x3A
)

func (p Proto) String() string {
	switch p {
	case ProtoICMP:
		return "ICMP"
	case ProtoTCP:
		return "TCP"
	case ProtoUDP:
		return "UDP"
	case ProtoICMPv6:
		return "ICMPv6"
	default:
		return fmt.Sprintf("0x%x", int(p))
	}
}

// HexUint32 is an integer that prefers to be printed in hexadecimal format
type HexUint32 uint32

func (h HexUint32) String() string {
	return fmt.Sprintf("0x%x", int(h))
}

// DecUint32 is an integer that prefers to be printed in hexadecimal format
type DecUint32 uint32

func (h DecUint32) String() string {
	return fmt.Sprintf("%d", int(h))
}

type DecUint64 uint64

func (h DecUint64) String() string {
	return fmt.Sprintf("%d", int(h))
}

// FlowKey is the struct of common fields that conform a flow
type FlowKey struct {
	FlowDirection FlowDirection

	// Interfaces
	InIf  DecUint32
	OutIf DecUint32
	// Ethernet Header
	SrcMac net.HardwareAddr
	DstMac net.HardwareAddr
	Etype  Etype
	// VLAN
	VlanID DecUint32
	// Network Header
	SrcAddr net.IP
	DstAddr net.IP
	Proto   Proto
	// TODO: Fragments
	// TODO MPLS

	// Transport Header
	SrcPort  DecUint32
	DstPort  DecUint32
	TCPFlags HexUint32
	ICMPType HexUint32
	ICMPCode HexUint32
}

func NewFlowKey(msg *flowmessage.FlowMessage) *FlowKey {
	return &FlowKey{
		FlowDirection: FlowDirection(msg.FlowDirection),
		InIf:          DecUint32(msg.InIf),
		OutIf:         DecUint32(msg.OutIf),
		SrcMac:        macFromUint64(msg.SrcMac),
		DstMac:        macFromUint64(msg.DstMac),
		Etype:         Etype(msg.Etype),
		VlanID:        DecUint32(msg.VlanId),
		SrcAddr:       ipFromBytes(msg.SrcAddr),
		DstAddr:       ipFromBytes(msg.DstAddr),
		Proto:         Proto(msg.Proto),
		SrcPort:       DecUint32(msg.SrcPort),
		DstPort:       DecUint32(msg.DstPort),
		TCPFlags:      HexUint32(msg.TCPFlags),
		ICMPType:      HexUint32(msg.IcmpType),
		ICMPCode:      HexUint32(msg.IcmpCode),
	}
}

// GetFieldString returns the string representation of the given fieldName
func (fk *FlowKey) GetFieldString(fieldName string) (string, error) {

	flowKeyV := reflect.ValueOf(fk).Elem()
	field := flowKeyV.FieldByName(fieldName)
	if !field.IsValid() {
		return "", fmt.Errorf("Failed to get Field %s from FlowKey", fieldName)
	}
	if field.Type().Kind() == reflect.Uint32 && fieldName != "FlowDirection" {
		return fmt.Sprintf("%d", int(field.Uint())), nil

	}
	return fmt.Sprintf("%s", field.Interface()), nil
}

// Matches returns whether another FlowKey is equal to this one
func (fk *FlowKey) Matches(other *FlowKey) bool {
	return reflect.DeepEqual(fk, other)
}

func macFromUint64(uintMac uint64) net.HardwareAddr {
	mac := make([]byte, 8)
	binary.BigEndian.PutUint64(mac, uintMac)
	return net.HardwareAddr(mac[2:])
}

func ipFromBytes(ipBytes []byte) net.IP {
	return net.IP(ipBytes)
}

// FlowInfo is a Flow metadata
type FlowInfo struct {
	Key               *FlowKey
	LastTimeReceived  DecUint64
	FirstTimeReceived DecUint64

	TotalBytes   DecUint64
	TotalPackets DecUint64

	LastBps      DecUint64
	LastDeltaBps DecUint64

	ForwardingStatus uint32
}

// Update updates the FlowInfo based on a FlowMessage
func (f *FlowInfo) Update(message *flowmessage.FlowMessage) {
	f.ForwardingStatus = message.ForwardingStatus

	f.TotalBytes += DecUint64(message.Bytes)
	f.TotalPackets += DecUint64(message.Packets)

	// FIXME: Should we use TimeFlowEnd?
	//var newBps DecUint64 = 0
	//if message.TimeFlowEnd != message.TimeFlowStart {
	//	newBps = DecUint64(message.Bytes / (message.TimeFlowEnd - message.TimeFlowStart))
	//}
	var newBps DecUint64 = 0
	if message.TimeReceived != uint64(f.LastTimeReceived) {
		newBps = DecUint64(message.Bytes / (message.TimeReceived - uint64(f.LastTimeReceived)))
	}

	f.LastTimeReceived = DecUint64(message.TimeReceived)
	f.LastDeltaBps = DecUint64(newBps - DecUint64(f.LastBps))
	f.LastBps = newBps
}

func NewFlowInfo(message *flowmessage.FlowMessage) *FlowInfo {
	return &FlowInfo{
		Key:               NewFlowKey(message),
		LastTimeReceived:  DecUint64(message.TimeReceived),
		FirstTimeReceived: DecUint64(message.TimeReceived),
		TotalBytes:        DecUint64(message.Bytes),
		TotalPackets:      DecUint64(message.Packets),
		LastBps:           0,
		LastDeltaBps:      0,
		ForwardingStatus:  message.ForwardingStatus,
	}
}
