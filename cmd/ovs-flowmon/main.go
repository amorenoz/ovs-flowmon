package main

import (
	"amorenoz/ovs-flowmon/pkg/flowmon"
	"context"
	"fmt"
	"net/url"
	"strconv"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/netsampler/goflow2/format"
	_ "github.com/netsampler/goflow2/format/protobuf"
	flowmessage "github.com/netsampler/goflow2/pb"
	"github.com/netsampler/goflow2/utils"
	"github.com/rivo/tview"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

var (
	tableView *tview.Table
	flowTable *FlowTable
	app       *tview.Application
	started   bool = false

	fieldList  []string = []string{"InIf", "OutIf", "SrcMac", "DstMac", "VlanID", "Etype", "SrcAddr", "DstAddr", "Proto", "SrcPort", "DstPort", "FlowDirection"}
	aggregates map[string]bool
)

type FlowTable struct {
	// mutex to protect aggregates (not flows)
	mutex      sync.RWMutex
	flows      []*flowmon.FlowInfo
	aggregates []*flowmon.FlowAggregate
	// Keeping both the list and the map for efficiency
	aggregateKeyList []string
	aggregateKeyMap  map[string]bool
	keys             []string
}

func NewFlowTable() *FlowTable {
	aggregateKeyList := []string{}
	for _, field := range fieldList {
		if aggregates[field] {
			aggregateKeyList = append(aggregateKeyList, field)
		}
	}
	return &FlowTable{
		mutex:            sync.RWMutex{},
		flows:            make([]*flowmon.FlowInfo, 0),
		aggregates:       make([]*flowmon.FlowAggregate, 0),
		aggregateKeyList: aggregateKeyList,
		aggregateKeyMap:  aggregates,
		keys:             fieldList,
	}
}

func (ft *FlowTable) UpdateKeys(aggregates map[string]bool) {
	aggregateKeyList := []string{}
	for _, field := range fieldList {
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

func (ft *FlowTable) Draw(tv *tview.Table) {
	var cell *tview.TableCell
	// Draw Key
	for col, key := range ft.keys {
		cell = tview.NewTableCell(key).SetTextColor(tcell.ColorWhite).SetAlign(tview.AlignLeft).SetSelectable(false)
		tv.SetCell(0, col, cell)
	}

	col := len(ft.keys)

	cell = tview.NewTableCell("TotalBytes").
		SetTextColor(tcell.ColorWhite).
		SetAlign(tview.AlignLeft).
		SetSelectable(false)
	tv.SetCell(0, col, cell)
	col += 1
	cell = tview.NewTableCell("TotalPackets").
		SetTextColor(tcell.ColorWhite).
		SetAlign(tview.AlignLeft).
		SetSelectable(false)
	tv.SetCell(0, col, cell)
	col += 1
	cell = tview.NewTableCell("Rate(kbps)").
		SetTextColor(tcell.ColorWhite).
		SetAlign(tview.AlignLeft).
		SetSelectable(false)
	tv.SetCell(0, col, cell)
	col += 1

	ft.mutex.RLock()
	defer ft.mutex.RUnlock()
	for i, agg := range ft.aggregates {
		for col, key := range fieldList {
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
			cell = tview.NewTableCell(fieldStr).SetTextColor(tcell.ColorWhite).SetAlign(tview.AlignLeft).SetSelectable(true)
			tv.SetCell(1+i, col, cell)
		}
		col := len(ft.keys)

		cell = tview.NewTableCell(fmt.Sprintf("%d", int(agg.TotalBytes))).
			SetTextColor(tcell.ColorWhite).
			SetAlign(tview.AlignLeft).
			SetSelectable(false)
		tv.SetCell(1+i, col, cell)
		col += 1
		cell = tview.NewTableCell(fmt.Sprintf("%d", int(agg.TotalPackets))).
			SetTextColor(tcell.ColorWhite).
			SetAlign(tview.AlignLeft).
			SetSelectable(false)
		tv.SetCell(1+i, col, cell)
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

		tv.SetCell(1+i, col, cell)
		col += 1
	}
}

func (ft *FlowTable) ProcessMessage(msg *flowmessage.FlowMessage) {
	log.Debugf("Processing Flow Message: %+v", msg)

	flowInfo := flowmon.NewFlowInfo(msg)
	ft.flows = append(ft.flows, flowInfo)

	ft.mutex.Lock()
	defer ft.mutex.Unlock()
	ft.ProcessFlow(flowInfo)
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

func readFlows(flowTable *FlowTable) {
	formatter, err := format.FindFormat(context.Background(), "pb")
	if err != nil {
		log.Fatal(err)
	}
	transporter := &Dispatcher{
		flowTable: flowTable,
	}
	Workers := 1
	ReusePort := false
	// wg.Add(1)
	go func(listenAddress string) {
		listenAddrUrl, err := url.Parse(listenAddress)
		if err != nil {
			log.Fatal(err)
		}

		hostname := listenAddrUrl.Hostname()
		port, err := strconv.ParseUint(listenAddrUrl.Port(), 10, 64)
		if err != nil {
			log.Errorf("Port %s could not be converted to integer", listenAddrUrl.Port())
			return
		}

		logFields := log.Fields{
			"scheme":   listenAddrUrl.Scheme,
			"hostname": hostname,
			"port":     port,
		}

		log.WithFields(logFields).Info("Starting collection")

		if listenAddrUrl.Scheme == "sflow" {
			sSFlow := &utils.StateSFlow{
				Format:    formatter,
				Transport: transporter,
				Logger:    log.StandardLogger(),
			}
			err = sSFlow.FlowRoutine(Workers, hostname, int(port), ReusePort)
		} else if listenAddrUrl.Scheme == "netflow" {
			sNF := &utils.StateNetFlow{
				Format:    formatter,
				Transport: transporter,
				Logger:    log.StandardLogger(),
			}
			err = sNF.FlowRoutine(Workers, hostname, int(port), ReusePort)
		} else if listenAddrUrl.Scheme == "nfl" {
			sNFL := &utils.StateNFLegacy{
				Format:    formatter,
				Transport: transporter,
				Logger:    log.StandardLogger(),
			}
			err = sNFL.FlowRoutine(Workers, hostname, int(port), ReusePort)
		} else {
			log.Errorf("scheme %s does not exist", listenAddrUrl.Scheme)
			return
		}

		if err != nil {
			log.WithFields(logFields).Fatal(err)
		}

	}("netflow://:2055")

	// wg.Wait()
}

// Implements TransportDriver
type Dispatcher struct {
	flowTable *FlowTable
}

func (d *Dispatcher) Prepare() error {
	return nil
}
func (d *Dispatcher) Init(context.Context) error {
	return nil
}
func (d *Dispatcher) Close(context.Context) error {
	return nil
}
func (d *Dispatcher) Send(key, data []byte) error {
	var msg flowmessage.FlowMessage
	if err := proto.Unmarshal(data, &msg); err != nil {
		log.Errorf("Wrong Flow Message (%s) : %s", err.Error(), string(data))
		return err
	}
	d.flowTable.ProcessMessage(&msg)
	app.QueueUpdateDraw(func() {
		d.flowTable.Draw(tableView)
	})
	return nil
}

type Listener interface {
	OnNewFlow(flow *flowmessage.FlowMessage)
}

func UpdateFlowTableKeys() {

}
func main() {
	aggregates = make(map[string]bool, 0)
	for _, key := range fieldList {
		aggregates[key] = true
	}
	app = tview.NewApplication()
	//pages := tview.NewPages()
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlC {
			app.Stop()
		}
		return event
	})
	tableView = tview.NewTable().
		SetSelectable(true, false). // Allow flows to be selected
		SetFixed(1, 1)              // Make it always focus the top left
	flowTable = NewFlowTable()
	status := tview.NewTextView().SetText("Stopped. Press Start to start capturing\n")
	log.SetOutput(status)

	// Top Menu
	menu := tview.NewFlex()
	menuList := tview.NewList().
		ShowSecondaryText(false)
	menu.AddItem(menuList, 0, 2, true)

	start := func() {
		log.Info("Started flow processing")
		if !started {
			readFlows(flowTable)
			started = true
		}
		app.SetFocus(tableView)
	}
	stop := func() {
		log.Info("Stopping")
		app.Stop()
	}
	logs := func() {
		app.SetFocus(status)
	}

	show_aggregate := func() {
		// Make columns selectable
		tableView.SetSelectable(false, true)
		app.SetFocus(tableView)
		tableView.SetSelectedFunc(func(row, col int) {
			colName := tableView.GetCell(0, col).Text
			// Toggle aggregate and update flow table
			aggregates[colName] = !aggregates[colName]
			flowTable.UpdateKeys(aggregates)
			tableView.Clear()
			flowTable.Draw(tableView)

			// Restore selectable columns and SelectedFunc
			tableView.SetSelectable(true, false)
			tableView.SetSelectedFunc(func(row, col int) {
				app.SetFocus(menuList)
			})
			app.SetFocus(menuList)
		})
	}

	menuList.AddItem("Start", "", 's', start).
		AddItem("Stop", "", 't', stop).
		AddItem("Logs", "", 'l', logs).
		AddItem("Add/Remove Fields from aggregate", "", 'a', show_aggregate)
	menuList.SetBorderPadding(1, 1, 2, 2)
	tableView.SetDoneFunc(func(key tcell.Key) {
		app.SetFocus(menuList)
	})
	tableView.SetSelectedFunc(func(row, col int) {
		app.SetFocus(menuList)
	})
	status.SetDoneFunc(func(key tcell.Key) {
		app.SetFocus(menuList)
	})
	menu.SetBorder(true).SetBorderPadding(1, 1, 2, 0).SetTitle("Menu")
	tableView.SetBorder(true).SetBorderPadding(1, 1, 2, 0).SetTitle("Flows")
	status.SetBorder(true).SetBorderPadding(1, 1, 2, 0).SetTitle("Logs")

	flex := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(menu, 0, 2, true).AddItem(tableView, 0, 5, false).AddItem(status, 0, 1, false)

	app.SetRoot(flex, true).SetFocus(flex)

	if err := app.Run(); err != nil {
		panic(err)
	}
}
