package view

import (
	"amorenoz/ovs-flowmon/pkg/flowmon"
	"fmt"
	"sync"

	"github.com/gdamore/tcell/v2"
	flowmessage "github.com/netsampler/goflow2/pb"
	"github.com/rivo/tview"
	log "github.com/sirupsen/logrus"
)

type SelectMode int

const ModeRows SelectMode = 0
const ModeCols SelectMode = 1

type FlowTable struct {
	View      *tview.Table
	StatsView *tview.Table
	// mutex to protect aggregates (not flows)
	mutex      sync.RWMutex
	flows      []*flowmon.FlowInfo
	aggregates []*flowmon.FlowAggregate
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
	ft.aggregates = make([]*flowmon.FlowAggregate, 0)
	for _, flow := range ft.flows {
		ft.ProcessFlow(flow)
	}
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

	// Restore selectable mode and SelectedFunc
	ft.SetSelectMode(ModeRows)

}

func (ft *FlowTable) SetSelectMode(mode SelectMode) {
	switch mode {
	case ModeRows:
		ft.View.SetSelectable(true, false)
	case ModeCols:
		ft.View.SetSelectable(false, true)
	}
	ft.mode = mode
	ft.Draw()
}

func (ft *FlowTable) Draw() {
	var cell *tview.TableCell
	// Draw Key
	for col, key := range ft.keys {
		cell = tview.NewTableCell(key).SetTextColor(tcell.ColorWhite).SetAlign(tview.AlignLeft).SetSelectable(ft.mode == ModeCols)
		ft.View.SetCell(0, col, cell)
	}

	col := len(ft.keys)

	cell = tview.NewTableCell("TotalBytes").
		SetTextColor(tcell.ColorWhite).
		SetAlign(tview.AlignLeft).
		SetSelectable(false)
	ft.View.SetCell(0, col, cell)
	col += 1
	cell = tview.NewTableCell("TotalPackets").
		SetTextColor(tcell.ColorWhite).
		SetAlign(tview.AlignLeft).
		SetSelectable(false)
	ft.View.SetCell(0, col, cell)
	col += 1
	cell = tview.NewTableCell("Rate(kbps)").
		SetTextColor(tcell.ColorWhite).
		SetAlign(tview.AlignLeft).
		SetSelectable(false)
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
	for _, agg := range ft.aggregates {
		matched, err = agg.AppendIfMatches(flowInfo)
		if err != nil {
			log.Error(err)
			return
		}
		if matched {
			break
		}
	}
	if !matched {
		// Create new Aggregate for this flow
		newAgg := flowmon.NewFlowAggregate(ft.aggregateKeyList)
		if match, err := newAgg.AppendIfMatches(flowInfo); !match || err != nil {
			log.Fatal(err)
		}
		ft.aggregates = append(ft.aggregates, newAgg)
	}
}
