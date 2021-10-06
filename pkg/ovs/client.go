package ovs

import (
	"context"
	"fmt"

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
	//	OtherConfig     map[string]string `ovsdb:"other_config"`
	OVSVersion *string `ovsdb:"ovs_version"`
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
	o.client.Close()
	return nil
}

func (o *OVSClient) SetIPFIX(bridgeName, target string, sampling, cacheMax, cacheTimeout int) error {
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
	log.Info(ops)
	response, err = o.client.Transact(context.TODO(), ops...)
	if err != nil {
		return err
	}
	if opErr, err := ovsdb.CheckOperationResults(response, ops); err != nil {
		return fmt.Errorf("%s: %+v", err.Error(), opErr)
	}
	return nil
}

func NewOVSClient(connStr string) (*OVSClient, error) {
	var err error
	model, err := model.NewDBModel("Open_vSwitch", map[string]model.Model{
		"Bridge":       &Bridge{},
		"IPFIX":        &IPFIX{},
		"Open_vSwitch": &OpenvSwitch{},
	})
	if err != nil {
		return nil, err
	}
	cli, err := client.NewOVSDBClient(model, client.WithEndpoint(connStr))
	if err != nil {
		return nil, err
	}
	err = cli.Connect(context.Background())
	if err != nil {
		return nil, err
	}
	_, err = cli.MonitorAll(context.TODO())
	if err != nil {
		return nil, err
	}

	return &OVSClient{
		client: cli,
	}, nil
}
