package flowmon

import "fmt"

// FlowAggregate is a list of flows aggregated by a set of keys
type FlowAggregate struct {
	Keys  []string
	Flows []*FlowInfo

	TotalBytes   DecUint64
	TotalPackets DecUint64

	LastTimeReceived   DecUint64
	FirstTimeReceived  DecUint64
	FirstTimeFlowStart DecUint64
	LastTimeFlowEnd    DecUint64

	LastBps      DecUint64
	LastDeltaBps int

	LastForwardingStatus uint32
}

func NewFlowAggregate(keys []string) *FlowAggregate {
	return &FlowAggregate{
		Keys:  keys,
		Flows: make([]*FlowInfo, 0),
	}
}

// Append appends the FlowInfo to the current Aggregate
func (fa *FlowAggregate) AppendIfMatches(flowInfo *FlowInfo) (bool, error) {
	match, err := fa.matches(flowInfo)
	if err != nil {
		return false, err
	}
	if !match {
		return false, nil
	}
	fa.LastForwardingStatus = flowInfo.ForwardingStatus

	fa.TotalBytes += DecUint64(flowInfo.Bytes)
	fa.TotalPackets += DecUint64(flowInfo.Packets)

	if fa.FirstTimeReceived == 0 {
		fa.FirstTimeReceived = DecUint64(flowInfo.TimeReceived)
	}
	if fa.FirstTimeFlowStart == 0 {
		fa.FirstTimeFlowStart = DecUint64(flowInfo.TimeFlowStart)
	}

	fa.LastTimeReceived = DecUint64(flowInfo.TimeReceived)
	fa.LastTimeFlowEnd = DecUint64(flowInfo.TimeFlowEnd)

	var newBps DecUint64 = 0
	if fa.LastTimeFlowEnd != fa.FirstTimeFlowStart {
		newBps = DecUint64(fa.TotalBytes / (fa.LastTimeFlowEnd - fa.FirstTimeFlowStart))
	}

	fa.LastDeltaBps = int(newBps) - int(fa.LastBps)
	fa.LastBps = newBps

	fa.Flows = append(fa.Flows, flowInfo)
	return true, nil
}

func (fa *FlowAggregate) matches(flowInfo *FlowInfo) (bool, error) {
	if len(fa.Flows) == 0 {
		// Accept new members to the aggregate if emtpy
		return true, nil
	}
	return fa.Flows[0].Key.Matches(flowInfo.Key, fa.Keys)
}

// GetFieldString returns the string representation of the given fieldName of the first flow
// Since only the first flow is used, it is assumed that only fields within the key list are
// used
func (fa *FlowAggregate) GetFieldString(fieldName string) (string, error) {
	if len(fa.Flows) == 0 {
		return "", fmt.Errorf("Empty Aggregate")
	}
	return fa.Flows[0].Key.GetFieldString(fieldName)
}
