package ovs

import (
	"amorenoz/ovs-flowmon/pkg/stats"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ovn-org/libovsdb/cache"
	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	log "github.com/sirupsen/logrus"
)

const (
	DefaultCacheMax      int = 0
	DefaultActiveTimeout int = 0
	DefaultSampling      int = 400
)

var (
	statNames map[string]string = map[string]string{
		"cpu":          "System Number of CPUs",
		"load_average": "System Load Average (1min, 5min, 15min)",
		"memory":       "System Memory (MiB) (total, allocated, flushable, total_swap, swap_in_use)",
		"ovs-virt":     "OVS Virtual memory (MiB)",
		"ovs-rss":      "OVS RSS (MiB)",
		"ovs-cpu":      "OVS CPU",
	}
	statOrder = []string{"cpu", "memory", "load_average", "ovs-virt", "ovs-rss", "ovs-cpu"}
)

// IPFIX defines an object in IPFIX table
type IPFIX struct {
	UUID               string            `ovsdb:"_uuid"`
	CacheActiveTimeout *int              `ovsdb:"cache_active_timeout"`
	CacheMaxFlows      *int              `ovsdb:"cache_max_flows"`
	ExternalIDs        map[string]string `ovsdb:"external_ids"`
	ObsDomainID        *int              `ovsdb:"obs_domain_id"`
	ObsPointID         *int              `ovsdb:"obs_point_id"`
	OtherConfig        map[string]string `ovsdb:"other_config"`
	Sampling           *int              `ovsdb:"sampling"`
	Targets            []string          `ovsdb:"targets"`
}

// OpenvSwitch defines an object in Open_vSwitch table
type OpenvSwitch struct {
	UUID    string   `ovsdb:"_uuid"`
	Bridges []string `ovsdb:"bridges"`
	//	CurCfg          int               `ovsdb:"cur_cfg"`
	//	DatapathTypes   []string          `ovsdb:"datapath_types"`
	//	Datapaths       map[string]string `ovsdb:"datapaths"`
	//	DbVersion       *string           `ovsdb:"db_version"`
	//	DpdkInitialized bool              `ovsdb:"dpdk_initialized"`
	//	DpdkVersion     *string           `ovsdb:"dpdk_version"`
	//	ExternalIDs     map[string]string `ovsdb:"external_ids"`
	//	IfaceTypes      []string          `ovsdb:"iface_types"`
	//	ManagerOptions  []string          `ovsdb:"manager_options"`
	//	NextCfg         int               `ovsdb:"next_cfg"`
	OtherConfig map[string]string `ovsdb:"other_config"`
	OVSVersion  *string           `ovsdb:"ovs_version"`
	//	SSL             *string           `ovsdb:"ssl"`
	Statistics map[string]string `ovsdb:"statistics"`
	//	SystemType      *string           `ovsdb:"system_type"`
	//	SystemVersion   *string           `ovsdb:"system_version"`
}

// Bridge defines an object in Bridge table
type Bridge struct {
	UUID string `ovsdb:"_uuid"`
	//	AutoAttach          *string           `ovsdb:"auto_attach"`
	//	Controller          []string          `ovsdb:"controller"`
	//	DatapathID          *string           `ovsdb:"datapath_id"`
	//	DatapathType        string            `ovsdb:"datapath_type"`
	//	DatapathVersion     string            `ovsdb:"datapath_version"`
	//	ExternalIDs         map[string]string `ovsdb:"external_ids"`
	//	FailMode            *BridgeFailMode   `ovsdb:"fail_mode"`
	//	FloodVLANs          [4096]int         `ovsdb:"flood_vlans"`
	//	FlowTables          map[int]string    `ovsdb:"flow_tables"`
	IPFIX *string `ovsdb:"ipfix"`
	//	McastSnoopingEnable bool              `ovsdb:"mcast_snooping_enable"`
	//	Mirrors             []string          `ovsdb:"mirrors"`
	Name    string  `ovsdb:"name"`
	Netflow *string `ovsdb:"netflow"`
	//	OtherConfig         map[string]string `ovsdb:"other_config"`
	Ports []string `ovsdb:"ports"`
	//Protocols           []BridgeProtocols `ovsdb:"protocols"`
	//RSTPEnable          bool              `ovsdb:"rstp_enable"`
	//RSTPStatus          map[string]string `ovsdb:"rstp_status"`
	Sflow  *string           `ovsdb:"sflow"`
	Status map[string]string `ovsdb:"status"`
	//STPEnable           bool              `ovsdb:"stp_enable"`
}

type OVSClient struct {
	client client.Client
	stats  stats.StatsBackend
}

func (o *OVSClient) Close() error {
	bridges := []Bridge{}

	if err := o.client.List(&bridges); err != nil {
		return err
	}
	for _, bridge := range bridges {
		if bridge.IPFIX == nil {
			continue
		}
		bridge.IPFIX = nil
		clearOps, err := o.client.Where(&bridge).Update(&bridge, &bridge.IPFIX)
		if err != nil {
			log.Error(err)
		} else {
			response, err := o.client.Transact(context.TODO(), clearOps...)
			if err != nil {
				log.Error(err)
			}
			if opErr, err := ovsdb.CheckOperationResults(response, clearOps); err != nil {
				log.Errorf("%s: %+v", err.Error(), opErr)
			}
		}
	}
	if err := o.DisableStatistics(); err != nil {
		log.Error(err)
	}
	o.client.Close()
	return nil
}

func (o *OVSClient) Started() bool {
	return o.client.Connected()
}

func (o *OVSClient) SetIPFIX(bridgeName, target string, sampling, cacheMax, cacheTimeout int) error {
	if !o.client.Connected() {
		return fmt.Errorf("Client not connected")
	}
	// Reconfigurations don't trigger a template event, to force it first delete
	// the current IPFIX config and only then create the new one
	bridge := &Bridge{
		Name:  bridgeName,
		IPFIX: nil,
	}
	clearOps, err := o.client.Where(bridge).Update(bridge, &bridge.IPFIX)
	if err != nil {
		return err
	}
	response, err := o.client.Transact(context.TODO(), clearOps...)
	if opErr, err := ovsdb.CheckOperationResults(response, clearOps); err != nil {
		log.Warnf("%s: %+v", err.Error(), opErr)
	}
	if err != nil {
		return err
	}

	// Create new configuration
	named := "id"
	ipfix := &IPFIX{
		UUID:               named,
		CacheActiveTimeout: &cacheTimeout,
		CacheMaxFlows:      &cacheMax,
		Sampling:           &sampling,
		Targets:            []string{target},
	}
	insertOps, err := o.client.Create(ipfix)
	if err != nil {
		return err
	}
	bridge.IPFIX = &ipfix.UUID
	updateOps, err := o.client.Where(bridge).Update(bridge, &bridge.IPFIX)
	if err != nil {
		return err
	}
	ops := append(insertOps, updateOps...)
	response, err = o.client.Transact(context.TODO(), ops...)
	logFields := log.Fields{
		"operation": ops,
		"response":  response,
		"err":       err,
	}
	log.WithFields(logFields).Debug("OVS IPFIX Configuration")

	if err != nil {
		return err
	}
	if opErr, err := ovsdb.CheckOperationResults(response, ops); err != nil {
		return fmt.Errorf("%s: %+v", err.Error(), opErr)
	}
	return nil
}

func NewOVSClient(connStr string, statsBackend stats.StatsBackend) (*OVSClient, error) {
	dbmodel, err := model.NewDBModel("Open_vSwitch", map[string]model.Model{
		"Bridge":       &Bridge{},
		"IPFIX":        &IPFIX{},
		"Open_vSwitch": &OpenvSwitch{},
	})
	if err != nil {
		return nil, err
	}
	cli, err := client.NewOVSDBClient(dbmodel, client.WithEndpoint(connStr))
	if err != nil {
		return nil, err
	}
	return &OVSClient{
		client: cli,
		stats:  statsBackend,
	}, nil
}

func (o *OVSClient) Start() error {
	if o.client.Connected() {
		return nil
	}
	err := o.client.Connect(context.Background())
	if err != nil {
		return err
	}
	_, err = o.client.MonitorAll(context.TODO())
	if err != nil {
		return err
	}
	return nil
}

func (o *OVSClient) EnableStatistics() error {
	for _, stat := range statOrder {
		o.stats.RegisterStat(statNames[stat])
	}

	// Enable statistics in OVS
	ovsList := []OpenvSwitch{}
	o.client.List(&ovsList)
	if len(ovsList) != 1 {
		return fmt.Errorf("Wrong number of entries in Open_vSwitch table")
	}
	ovs := ovsList[0]
	mutateOps, err := o.client.Where(&ovs).Mutate(&ovs, model.Mutation{
		Field:   &ovs.OtherConfig,
		Mutator: ovsdb.MutateOperationInsert,
		Value:   map[string]string{"enable-statistics": "true"},
	})
	if err != nil {
		return err
	}
	response, err := o.client.Transact(context.TODO(), mutateOps...)
	logFields := log.Fields{
		"operation": mutateOps,
		"response":  response,
		"err":       err,
	}
	log.WithFields(logFields).Debug("OVS Statistics Enabling")
	if err != nil {
		log.Error(err)
	}
	if opErr, err := ovsdb.CheckOperationResults(response, mutateOps); err != nil {
		log.Errorf("%s: %+v", err.Error(), opErr)
	}

	// Register update callback
	o.client.Cache().AddEventHandler(&cache.EventHandlerFuncs{
		UpdateFunc: func(table string, oldModel, newModel model.Model) {
			if table == "Open_vSwitch" {
				newOvs := newModel.(*OpenvSwitch)
				oldOvs := oldModel.(*OpenvSwitch)
				o.updateStatistics(oldOvs.Statistics, newOvs.Statistics)
			}
		},
	})
	return nil
}

func (o *OVSClient) DisableStatistics() error {
	ovsList := []OpenvSwitch{}
	o.client.List(&ovsList)
	if len(ovsList) != 1 {
		return fmt.Errorf("Wrong number of entries in Open_vSwitch table")
	}
	ovs := ovsList[0]
	mutateOps, err := o.client.Where(&ovs).Mutate(&ovs, model.Mutation{
		Field:   &ovs.OtherConfig,
		Mutator: ovsdb.MutateOperationInsert,
		Value:   map[string]string{"enable-statistics": "false"},
	})
	if err != nil {
		return err
	}
	response, err := o.client.Transact(context.TODO(), mutateOps...)
	logFields := log.Fields{
		"operation": mutateOps,
		"response":  response,
		"err":       err,
	}
	log.WithFields(logFields).Debug("OVS Statistics Enabling")
	if err != nil {
		log.Error(err)
	}
	if opErr, err := ovsdb.CheckOperationResults(response, mutateOps); err != nil {
		log.Errorf("%s: %+v", err.Error(), opErr)
	}
	return nil
}

func (o *OVSClient) updateStatistics(old_statistics, statistics map[string]string) {
	logFields := log.Fields{
		"old": old_statistics,
		"new": statistics,
	}
	log.WithFields(logFields).Debug("Updating Statistics")

	if cpu, ok := statistics["cpu"]; ok {
		o.stats.UpdateStat(statNames["cpu"], cpu)
	}
	if load, ok := statistics["load_average"]; ok {
		o.stats.UpdateStat(statNames["load_average"], load)
	}
	if mem, ok := statistics["memory"]; ok {
		mem_stat := []string{}
		for _, field := range strings.Split(mem, ",") {
			float_val, err := strconv.ParseFloat(field, 64)
			if err != nil {
				log.Error(err)
				continue
			}
			mem_stat = append(mem_stat, fmt.Sprintf("%.2f", float_val/1024))
		}
		o.stats.UpdateStat(statNames["memory"], strings.Join(mem_stat, ","))
	}
	o.updateProcessStatistics(old_statistics, statistics)
	o.stats.Draw()
}

func (o *OVSClient) updateProcessStatistics(old_statistics, statistics map[string]string) {
	var virt, rss, cpu, total, old_cpu, old_total int64
	ovs, ok := statistics["process_ovs-vswitchd"]
	ovs_stats := strings.Split(ovs, ",")

	if old_ovs, ok := old_statistics["process_ovs-vswitchd"]; ok {
		old_ovs_stat := strings.Split(old_ovs, ",")
		old_cpu, _ = strconv.ParseInt(old_ovs_stat[2], 10, 64)
		old_total, _ = strconv.ParseInt(old_ovs_stat[5], 10, 64)
	}
	if !ok {
		return
	}
	virt, _ = strconv.ParseInt(ovs_stats[0], 10, 64)
	rss, _ = strconv.ParseInt(ovs_stats[1], 10, 64)
	cpu, _ = strconv.ParseInt(ovs_stats[2], 10, 64)
	total, _ = strconv.ParseInt(ovs_stats[5], 10, 64)
	cpu_percent := 100 * float64(cpu-old_cpu) / float64(total-old_total)
	if old_total == total || cpu == old_cpu {
		return
	}

	o.stats.UpdateStat(statNames["ovs-virt"], fmt.Sprintf("%.2f", float64(virt)/1024))
	o.stats.UpdateStat(statNames["ovs-rss"], fmt.Sprintf("%.2f", float64(rss)/1024))
	o.stats.UpdateStat(statNames["ovs-cpu"], fmt.Sprintf("%.2f", cpu_percent))
}
