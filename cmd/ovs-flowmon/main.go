package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"amorenoz/ovs-flowmon/pkg/ovs"
	"amorenoz/ovs-flowmon/pkg/stats"
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
	app         *tview.Application
	flowTable   *view.FlowTable
	statsViewer *stats.StatsView
	ovsClient   *ovs.OVSClient

	fieldList []string = []string{"InIf", "OutIf", "SrcMac", "DstMac", "VlanID", "Etype", "SrcAddr", "DstAddr", "Proto", "SrcPort", "DstPort", "FlowDirection"}
	ovsdb              = flag.String("ovsdb", "", "Enable OVS configuration by providing a database string, e.g: unix:/var/run/openvswitch/db.sock")
	iface              = flag.String("iface", "", "Interface name where to listen. If ovsdb configuration is enabled"+
		"The IPv4 address of this interface will be used as target, so make sure the remote vswitchd can reach it")
	logLevel = flag.String("loglevel", "info", "Log level")
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

	ipAddress := ""
	if *ovsdb != "" {
		ipAddress = ipAddressFromOvsdb(*ovsdb)
	}
	listen := "netflow://" + ipAddress + ":2055"
	log.Infof("Listening on %s", listen)

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

		log.WithFields(logFields).Info("Starting collection on " + listenAddress)

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

	}(listen)

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

func ipAddressFromOvsdb(ovsdb string) string {
	parts := strings.Split(ovsdb, ":")
	switch parts[0] {
	case "tcp":
		conn, err := net.Dial("tcp", strings.Join(parts[1:], ":"))
		if err != nil {
			panic(err)
		}
		return strings.Split(conn.LocalAddr().String(), ":")[0]
	case "unix":
		return "127.0.0.1"
	}
	return ""
}

func welcomePage(pages *tview.Pages) {
	welcome := tview.NewModal().SetText(`

Welcome to OvS Flow Monitor!

If an OvS vSwitch is present, you can start, stop and reconfigure the IPFIX Exporter from the main menu. If not, please start an IPFIX Exporter that sends flows to tcp::2055.

Note that if you had already started the IPFIX exporter, it might take some time (e.g: 10mins in OvS) before it sends us the Templates, without which we cannot
decode the IPFIX Flow Records. It is possible that re-starting the exporter helps.


`).AddButtons([]string{"Start"}).SetDoneFunc(func(index int, label string) {
		pages.HidePage("welcome")
		pages.ShowPage("main")
	})
	pages.AddPage("welcome", welcome, true, true)
}

func mainPage(pages *tview.Pages) {
	aggregates := make(map[string]bool, 0)
	for _, key := range fieldList {
		aggregates[key] = true
	}
	statsViewer = stats.NewStatsView(app)
	flowTable = view.NewFlowTable(fieldList, aggregates, statsViewer)
	status := tview.NewTextView().SetText("Stopped. Press Start to start capturing\n")
	log.SetOutput(status)

	// Top Menu
	menu := tview.NewFlex()
	menuList := tview.NewList().
		ShowSecondaryText(false)
	menu.AddItem(menuList, 0, 2, true)
	menu.AddItem(statsViewer.View(), 0, 2, false)

	exit := func() {
		exit()
	}
	logs := func() {
		app.SetFocus(status)
	}
	flows := func() {
		app.SetFocus(flowTable.View)
	}

	show_aggregate := func() {
		// Make columns selectable
		flowTable.SetSelectMode(view.ModeColsKeys)
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
		flowTable.SetSelectMode(view.ModeColsAll)
		app.SetFocus(flowTable.View)
		flowTable.View.SetSelectedFunc(func(row, col int) {
			err := flowTable.SetSortingColumn(col)
			if err != nil {
				log.Error(err)
			}
			flowTable.SetSelectMode(view.ModeRows)
			flowTable.View.SetSelectedFunc(func(row, col int) {
				app.SetFocus(menuList)
			})
			app.SetFocus(menuList)
		})
	}

	if *ovsdb != "" {
		menuList.AddItem("Start OvS IPFIX Exporter", "", 's', func() {
			ovs_start("br-int", ovs.DefaultSampling, ovs.DefaultCacheMax, ovs.DefaultActiveTimeout)
		})
		menuList.AddItem("(Re)Configure OvS IPFIX Exporter", "", 'c', func() {
			pages.ShowPage("config")
		})
		menuList.AddItem("Stop OvS IPFIX Exporter", "", 't', func() {
			ovs_stop()
		})
	}
	menuList.AddItem("Flows", "", 'f', flows).
		AddItem("Add/Remove Fields from aggregate", "", 'a', show_aggregate).
		AddItem("Sort by ", "", 's', sort_by).
		AddItem("Logs", "", 'l', logs).
		AddItem("Exit", "", 'e', exit)
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

	flex := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(menu, 0, 2, true).AddItem(flowTable.View, 0, 5, false).AddItem(status, 0, 1, false)

	readFlows(flowTable)
	pages.AddPage("main", flex, true, false)
}

func center(p tview.Primitive, width, height int) tview.Primitive {
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(p, height, 1, true).
			AddItem(nil, 0, 1, false), width, 1, true).
		AddItem(nil, 0, 1, false)
}
func configPage(pages *tview.Pages) {
	var err error
	if *ovsdb == "" {
		return
	}
	// Initialize OVS Configuration client
	ovsClient, err = ovs.NewOVSClient(*ovsdb, statsViewer)
	if err != nil {
		fmt.Print(err)
		log.Fatal(err)
	}

	sampling := 400
	bridge := "br-int"
	form := tview.NewForm()
	form.AddDropDown("Bridge", []string{"br-int"}, 0, func(option string, _ int) {
		bridge = option
	}). // TODO: Add more
		AddInputField("Sampling", "400", 5, func(textToCheck string, _ rune) bool {
			_, err := strconv.ParseInt(textToCheck, 0, 32)
			return err == nil
		}, func(text string) {
			intVal, err := strconv.ParseInt(text, 0, 32)
			if err == nil {
				sampling = int(intVal)
			}
		}).
		AddButton("Save", func() {
			ovs_start(bridge, sampling, ovs.DefaultCacheMax, ovs.DefaultActiveTimeout)
			pages.HidePage("config")
		}).
		AddButton("Cancel", func() {
			pages.HidePage("config")
		})
	// TODO add cache size, etc
	configMenu := tview.NewFlex()
	configMenu.SetTitle("OVS Configuration").SetBorder(true)
	configMenu.SetDirection(tview.FlexRow).AddItem(tview.NewTextView().SetText(`Configure OvS IPFIX Exporter

Use <Tab> to move around the form
Press <Save> to save the configuration
Press <Cancel> to go back to the main menu


`), 0, 1, false).
		AddItem(form, 0, 2, true)
	pages.AddPage("config", center(configMenu, 60, 20), true, false)
}

func ovs_stop() {
	if ovsClient != nil {
		log.Info("Stopping IPFIX exporter")
		err := ovsClient.Close()
		if err != nil {
			log.Error(err)
		}
	}
}

func ovs_start(bridge string, sampling, cacheMax, cacheTimeout int) {
	if !ovsClient.Started() {
		err := ovsClient.Start()
		if err != nil {
			log.Error("OVSDB not configured")
			return
		}
		ovsClient.EnableStatistics()
		if err != nil {
			fmt.Print(err)
			log.Fatal(err)
		}
	}
	target := ipAddressFromOvsdb(*ovsdb) + ":2055"
	err := ovsClient.SetIPFIX(bridge, target, sampling, cacheMax, cacheTimeout)
	if err != nil {
		log.Error("Failed to set OVS configuration")
		log.Error(err)
	} else {
		log.Info("OVS configuration changed")
	}
}

func exit() {
	log.Info("Stopping")
	ovs_stop()
	if app != nil {
		app.Stop()
	}
}

func main() {
	flag.Parse()
	lvl, _ := log.ParseLevel(*logLevel)
	log.SetLevel(lvl)

	app = tview.NewApplication()
	pages := tview.NewPages()
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlC {
			exit()
		}
		return event
	})

	mainPage(pages)
	configPage(pages)
	welcomePage(pages)

	app.SetRoot(pages, true).SetFocus(pages)

	if err := app.Run(); err != nil {
		panic(err)
	}
}
