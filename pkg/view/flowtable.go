package view

import (
	"amorenoz/ovs-flowmon/pkg/flowmon"
	"fmt"
	"sort"
	"sync"

	"github.com/gdamore/tcell/v2"
	flowmessage "github.com/netsampler/goflow2/pb"
	"github.com/rivo/tview"
	log "github.com/sirupsen/logrus"
)

type SelectMode int

const ModeRows SelectMode = 0     // Only Rows are selectable
const ModeColsAll SelectMode = 1  // All Columns are selectable
const ModeColsKeys SelectMode = 2 // Only Flow Key columns are selectable

type FlowTable struct {
	View      *tview.Table
	StatsView *tview.Table
	// mutex to protect aggregates (not flows)
	mutex sync.RWMutex

	// data
	flows      []*flowmon.FlowInfo
	aggregates []*flowmon.FlowAggregate
	lessFunc   func(one, other *flowmon.FlowAggregate) bool

	// configuration
	// Keeping both the list and the map for efficiency
	aggregateKeyList []string
	aggregateKeyMap  map[string]bool
	keys             []string
	mode             SelectMode

	// Stats
	nMessages int
}

func NewFlowTable(fields []string, aggregates map[string]bool) *FlowTable {
	aggregateKeyList := []string{}
	for _, field := range fields {
		if aggregates[field] {
			aggregateKeyList = append(aggregateKeyList, field)
		}
	}
	tableView := tview.NewTable().
		SetSelectable(true, false). // Allow flows to be selected
		SetFixed(1, 1).             // Make it always focus the top left
		SetSelectable(true, false)  // Start in RowMode

	stats := tview.NewTable().
		SetCell(0, 0, tview.NewTableCell("Processed Messages: ")).
		SetCell(0, 1, tview.NewTableCell(fmt.Sprintf("%d", 0)))

	return &FlowTable{
		View:             tableView,
		StatsView:        stats,
		mutex:            sync.RWMutex{},
		flows:            make([]*flowmon.FlowInfo, 0),
		aggregates:       make([]*flowmon.FlowAggregate, 0),
		aggregateKeyList: aggregateKeyList,
		aggregateKeyMap:  aggregates,
		keys:             fields,
		lessFunc: func(one, other *flowmon.FlowAggregate) bool {
			return one.LastTimeReceived < other.LastTimeReceived
		},
	}
}

func (ft *FlowTable) GetAggregates() map[string]bool {
	return ft.aggregateKeyMap
}

func (ft *FlowTable) UpdateKeys(aggregates map[string]bool) {
	aggregateKeyList := []string{}
	for _, field := range ft.keys {
		if aggregates[field] {
			aggregateKeyList = append(aggregateKeyList, field)
		}
	}
	ft.mutex.Lock()
	defer ft.mutex.Unlock()
	ft.aggregateKeyList = aggregateKeyList
	ft.aggregateKeyMap = aggregates
	// Need to recompute all aggregations
	ft.recompute()
}

func (ft *FlowTable) ToggleAggregate(index int) {
	colName := ft.View.GetCell(0, index).Text
	// Toggle aggregate and update flow table
	newAggregates := make(map[string]bool)
	for k, v := range ft.aggregateKeyMap {
		newAggregates[k] = v
	}
	newAggregates[colName] = !newAggregates[colName]
	ft.UpdateKeys(newAggregates)
	ft.View.Clear()
	ft.Draw()
}

func (ft *FlowTable) SetSelectMode(mode SelectMode) {
	switch mode {
	case ModeRows:
		ft.View.SetSelectable(true, false)
	case ModeColsAll, ModeColsKeys:
		ft.View.SetSelectable(false, true)
	}
	ft.mode = mode
	ft.Draw()
}

func (ft *FlowTable) Draw() {
	var cell *tview.TableCell
	// Draw Key
	for col, key := range ft.keys {
		cell = tview.NewTableCell(key).SetTextColor(tcell.ColorWhite).SetAlign(tview.AlignLeft).SetSelectable(ft.mode != ModeRows)
		ft.View.SetCell(0, col, cell)
	}

	col := len(ft.keys)

	cell = tview.NewTableCell("TotalBytes").
		SetTextColor(tcell.ColorWhite).
		SetAlign(tview.AlignLeft).
		SetSelectable(ft.mode == ModeColsAll)
	ft.View.SetCell(0, col, cell)
	col += 1
	cell = tview.NewTableCell("TotalPackets").
		SetTextColor(tcell.ColorWhite).
		SetAlign(tview.AlignLeft).
		SetSelectable(ft.mode == ModeColsAll)
	ft.View.SetCell(0, col, cell)
	col += 1
	cell = tview.NewTableCell("Rate(kbps)").
		SetTextColor(tcell.ColorWhite).
		SetAlign(tview.AlignLeft).
		SetSelectable(ft.mode == ModeColsAll)
	ft.View.SetCell(0, col, cell)
	col += 1

	ft.mutex.RLock()
	defer ft.mutex.RUnlock()
	for i, agg := range ft.aggregates {
		for col, key := range ft.keys {
			var fieldStr string
			var err error
			if !ft.aggregateKeyMap[key] {
				fieldStr = "-"
			} else {
				fieldStr, err = agg.GetFieldString(key)
				if err != nil {
					log.Error(err)
					fieldStr = "err"
				}
			}
			cell = tview.NewTableCell(fieldStr).SetTextColor(tcell.ColorWhite).SetAlign(tview.AlignLeft).SetSelectable(ft.mode == ModeRows)
			ft.View.SetCell(1+i, col, cell)
		}
		col := len(ft.keys)

		cell = tview.NewTableCell(fmt.Sprintf("%d", int(agg.TotalBytes))).
			SetTextColor(tcell.ColorWhite).
			SetAlign(tview.AlignLeft).
			SetSelectable(false)
		ft.View.SetCell(1+i, col, cell)
		col += 1
		cell = tview.NewTableCell(fmt.Sprintf("%d", int(agg.TotalPackets))).
			SetTextColor(tcell.ColorWhite).
			SetAlign(tview.AlignLeft).
			SetSelectable(false)
		ft.View.SetCell(1+i, col, cell)
		col += 1

		delta := "="
		if agg.LastDeltaBps > 0 {
			delta = "↑"
		} else if agg.LastDeltaBps < 0 {
			delta = "↓"
		}
		cell = tview.NewTableCell(fmt.Sprintf("%.1f %s", float64(agg.LastBps)/1000, delta)).
			SetTextColor(tcell.ColorWhite).
			SetAlign(tview.AlignLeft).
			SetSelectable(false)

		ft.View.SetCell(1+i, col, cell)
		col += 1
	}
	// Draw Stats
	ft.StatsView.SetCell(0, 1, tview.NewTableCell(fmt.Sprintf("%d", ft.nMessages)))
}

func (ft *FlowTable) ProcessMessage(msg *flowmessage.FlowMessage) {
	log.Debugf("Processing Flow Message: %+v", msg)

	flowInfo := flowmon.NewFlowInfo(msg)
	ft.flows = append(ft.flows, flowInfo)

	ft.mutex.Lock()
	defer ft.mutex.Unlock()
	ft.ProcessFlow(flowInfo)
	ft.nMessages += 1
}

// Caller must hold mutex
func (ft *FlowTable) ProcessFlow(flowInfo *flowmon.FlowInfo) {
	var matched bool = false
	var err error = nil
	for i, agg := range ft.aggregates {
		matched, err = agg.AppendIfMatches(flowInfo)
		if err != nil {
			log.Error(err)
			return
		}
		if matched {
			// Re-insert the matched aggregate
			ft.aggregates = append(ft.aggregates[:i], ft.aggregates[i+1:]...)
			ft.insertSortedAggregate(agg)
			break
		}
	}
	if !matched {
		// Create new Aggregate for this flow
		newAgg := flowmon.NewFlowAggregate(ft.aggregateKeyList)
		if match, err := newAgg.AppendIfMatches(flowInfo); !match || err != nil {
			log.Fatal(err)
		}

		// Sorted insertion
		ft.insertSortedAggregate(newAgg)
	}
}

func (ft *FlowTable) insertSortedAggregate(agg *flowmon.FlowAggregate) {
	insertionPoint := sort.Search(len(ft.aggregates), func(i int) bool {
		return ft.lessFunc(agg, ft.aggregates[i])
	})
	if insertionPoint == len(ft.aggregates) {
		ft.aggregates = append(ft.aggregates, agg)
	} else {
		ft.aggregates = append(ft.aggregates[0:insertionPoint],
			append([]*flowmon.FlowAggregate{agg}, ft.aggregates[insertionPoint:]...)...)
	}
}

// SetSortingKey sets the field that will be used for sorting the aggregates
func (ft *FlowTable) SetSortingColumn(index int) error {
	colName := ft.View.GetCell(0, index).Text
	return ft.SetSortingKey(colName)
}

// SetSortingKey sets the field that will be used for sorting the aggregates
func (ft *FlowTable) SetSortingKey(key string) error {
	switch key {
	case "LastTimeReceived":
		ft.lessFunc = func(one, other *flowmon.FlowAggregate) bool {
			return one.LastTimeReceived < other.LastTimeReceived
		}
	case "Rate(kbps)":
		ft.lessFunc = func(one, other *flowmon.FlowAggregate) bool {
			return one.LastBps < other.LastBps
		}
	case "TotalBytes":
		ft.lessFunc = func(one, other *flowmon.FlowAggregate) bool {
			return one.TotalBytes < other.TotalBytes
		}
	case "TotalPackets":
		ft.lessFunc = func(one, other *flowmon.FlowAggregate) bool {
			return one.TotalBytes < other.TotalBytes
		}

	default:
		found := false
		for _, k := range ft.keys {
			if k == key {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("Cannot set sorting key to %s", key)
		}
		if !ft.aggregateKeyMap[key] {
			return fmt.Errorf("Cannot set sorting key to %s since it's not part of the aggregate", key)
		}
		ft.lessFunc = func(one, other *flowmon.FlowAggregate) bool {
			res, _ := one.Less(key, other)
			return res
		}
	}

	ft.mutex.Lock()
	ft.recompute()
	ft.mutex.Unlock()
	// Do not hold the lock while redrawing

	ft.View.Clear()
	ft.Draw()
	return nil
}

// Recompute all aggregates
func (ft *FlowTable) recompute() {
	ft.aggregates = make([]*flowmon.FlowAggregate, 0)
	for _, flow := range ft.flows {
		ft.ProcessFlow(flow)
	}
}
