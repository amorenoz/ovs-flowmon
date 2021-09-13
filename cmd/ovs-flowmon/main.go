package main

import (
	"amorenoz/ovs-flowmon/pkg/flowmon"
	"context"
	"fmt"
	"net/url"
	"strconv"

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

	default_fields []string = []string{"InIf", "OutIf", "SrcMac", "DstMac", "VlanID", "Etype", "SrcAddr", "DstAddr", "Proto", "SrcPort", "DstPort", "FlowDirection"}
)

type FlowTable struct {
	flows []*flowmon.FlowInfo
	keys  []string
}

func NewFlowTable() *FlowTable {
	return &FlowTable{
		flows: make([]*flowmon.FlowInfo, 0),
		keys:  default_fields,
	}
}

func (ft *FlowTable) InsertFlow(f *flowmon.FlowInfo) {
	//ft.flows = append(ft.flows, f)
	// TODO: Different kind of sorting
	// For now, prepend the last received flow at the top
	log.Debugf("Inserting %#v", f)
	ft.flows = append([]*flowmon.FlowInfo{f}, ft.flows...)
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
	cell = tview.NewTableCell("kbps").
		SetTextColor(tcell.ColorWhite).
		SetAlign(tview.AlignLeft).
		SetSelectable(false)
	tv.SetCell(0, col, cell)
	col += 1

	for i, flow := range ft.flows {
		for col, key := range ft.keys {
			fieldStr, err := flow.Key.GetFieldString(key)
			if err != nil {
				log.Error(err)
				fieldStr = "err"
			}
			cell = tview.NewTableCell(fieldStr).SetTextColor(tcell.ColorWhite).SetAlign(tview.AlignLeft).SetSelectable(true)
			tv.SetCell(1+i, col, cell)
		}
		col := len(ft.keys)

		cell = tview.NewTableCell(fmt.Sprintf("%d", int(flow.TotalBytes))).
			SetTextColor(tcell.ColorWhite).
			SetAlign(tview.AlignLeft).
			SetSelectable(true)
		tv.SetCell(1+i, col, cell)
		col += 1
		cell = tview.NewTableCell(fmt.Sprintf("%d", int(flow.TotalPackets))).
			SetTextColor(tcell.ColorWhite).
			SetAlign(tview.AlignLeft).
			SetSelectable(false)
		tv.SetCell(1+i, col, cell)
		col += 1

		delta := "="
		if flow.LastDeltaBps > 0 {
			delta = "↑"
		} else if flow.LastDeltaBps < 0 {
			delta = "↓"
		}
		cell = tview.NewTableCell(fmt.Sprintf("%f %s", float64(flow.LastBps/1000), delta)).
			SetTextColor(tcell.ColorWhite).
			SetAlign(tview.AlignLeft).
			SetSelectable(true)

		tv.SetCell(1+i, col, cell)
		col += 1
	}
}

func (ft *FlowTable) ProcessFlow(msg *flowmessage.FlowMessage) {
	log.Infof("Processing Flow Message: %+v", msg)
	matched := false
	flowKey := flowmon.NewFlowKey(msg)
	for _, flowInfo := range ft.flows {
		if flowInfo.Key.Matches(flowKey) {
			flowInfo.Update(msg)
			matched = true
			break
		}
	}
	if !matched {
		ft.InsertFlow(flowmon.NewFlowInfo(msg))
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

// Implements FormatDriver

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
	d.flowTable.ProcessFlow(&msg)
	app.QueueUpdateDraw(func() {
		d.flowTable.Draw(tableView)
	})
	return nil
}

type Listener interface {
	OnNewFlow(flow *flowmessage.FlowMessage)
}

func main() {
	app = tview.NewApplication()
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlC {
			app.Stop()
		}
		return event
	})
	tableView = tview.NewTable()
	flowTable = NewFlowTable()
	status := tview.NewTextView().SetText("Stopped. Press Start to start capturing\n")
	log.SetOutput(status)

	// Top Menu
	start := func() {
		log.Info("Started flow processing")
		readFlows(flowTable)
		app.SetFocus(tableView)
	}
	stop := func() {
		log.Info("Stopping")
		app.Stop()
	}
	logs := func() {
		app.SetFocus(status)
	}
	menuList := tview.NewList().ShowSecondaryText(false).
		AddItem("Start", "", 's', start).
		AddItem("Stop", "", 't', stop).
		AddItem("Logs", "", 'l', logs)
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

	flex := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(menuList, 0, 2, true).AddItem(tableView, 0, 5, false).AddItem(status, 0, 1, false)

	app.SetRoot(flex, true).SetFocus(flex)

	if err := app.Run(); err != nil {
		panic(err)
	}
}
