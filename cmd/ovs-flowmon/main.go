package main

import (
	"context"
	"net/url"
	"strconv"

	"amorenoz/ovs-flowmon/pkg/view"

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
	app       *tview.Application
	flowTable *view.FlowTable

	started bool = false

	fieldList []string = []string{"InIf", "OutIf", "SrcMac", "DstMac", "VlanID", "Etype", "SrcAddr", "DstAddr", "Proto", "SrcPort", "DstPort", "FlowDirection"}
)

func readFlows(flowTable *view.FlowTable) {
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
	flowTable *view.FlowTable
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
		d.flowTable.Draw()
	})
	return nil
}

type Listener interface {
	OnNewFlow(flow *flowmessage.FlowMessage)
}

func main() {
	aggregates := make(map[string]bool, 0)
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

	flowTable = view.NewFlowTable(fieldList, aggregates)
	status := tview.NewTextView().SetText("Stopped. Press Start to start capturing\n")
	log.SetOutput(status)

	// Top Menu
	menu := tview.NewFlex()
	menuList := tview.NewList().
		ShowSecondaryText(false)
	menu.AddItem(menuList, 0, 2, true)
	menu.AddItem(flowTable.StatsView, 0, 2, false)

	start := func() {
		log.Info("Started flow processing")
		if !started {
			readFlows(flowTable)
			started = true
		}
		app.SetFocus(flowTable.View)
	}
	stop := func() {
		log.Info("Stopping")
		app.Stop()
	}
	logs := func() {
		app.SetFocus(status)
	}
	flows := func() {
		app.SetFocus(flowTable.View)
	}

	show_aggregate := func() {
		// Make columns selectable
		flowTable.SetSelectMode(view.ModeCols)
		app.SetFocus(flowTable.View)
		flowTable.View.SetSelectedFunc(func(row, col int) {
			flowTable.ToggleAggregate(col)
			flowTable.SetSelectMode(view.ModeRows)
			flowTable.View.SetSelectedFunc(func(row, col int) {
				app.SetFocus(menuList)
			})
			app.SetFocus(menuList)
		})
	}

	sort_by := func() {
		// Make columns selectable
		flowTable.SetSelectMode(view.ModeCols)
		app.SetFocus(flowTable.View)
		flowTable.View.SetSelectedFunc(func(row, col int) {
			flowTable.SetSortingColumn(col)
			flowTable.SetSelectMode(view.ModeRows)
			flowTable.View.SetSelectedFunc(func(row, col int) {
				app.SetFocus(menuList)
			})
			app.SetFocus(menuList)
		})
	}

	menuList.AddItem("Start", "", 's', start).
		AddItem("Stop", "", 't', stop).
		AddItem("Flows", "", 'f', flows).
		AddItem("Logs", "", 'l', logs).
		AddItem("Add/Remove Fields from aggregate", "", 'a', show_aggregate).
		AddItem("Sort by ", "", 's', sort_by)
	flowTable.View.SetDoneFunc(func(key tcell.Key) {
		app.SetFocus(menuList)
	})
	flowTable.View.SetSelectedFunc(func(row, col int) {
		app.SetFocus(menuList)
	})
	status.SetDoneFunc(func(key tcell.Key) {
		app.SetFocus(menuList)
	})
	menuList.SetBorder(true).SetBorderPadding(1, 1, 2, 0).SetTitle("Menu")
	flowTable.View.SetBorder(true).SetBorderPadding(1, 1, 2, 0).SetTitle("Flows")
	status.SetBorder(true).SetBorderPadding(1, 1, 2, 0).SetTitle("Logs")
	flowTable.StatsView.SetBorder(true).SetBorderPadding(1, 1, 2, 0).SetTitle("Stats")

	flex := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(menu, 0, 2, true).AddItem(flowTable.View, 0, 5, false).AddItem(status, 0, 1, false)

	app.SetRoot(flex, true).SetFocus(flex)

	if err := app.Run(); err != nil {
		panic(err)
	}
}
