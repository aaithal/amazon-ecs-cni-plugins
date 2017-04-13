package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"runtime"
	"syscall"

	"github.com/aws/amazon-ecs-cni-plugins/pkg/logger"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/version"
	log "github.com/cihub/seelog"
	"github.com/containernetworking/cni/pkg/ip"
	"github.com/containernetworking/cni/pkg/ns"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	cniversion "github.com/containernetworking/cni/pkg/version"
	"github.com/pkg/errors"
	"github.com/vishvananda/netlink"
)

const (
	defaultLogFilePath = "/var/log/ecs/ecs-cni-bridge-plugin.log"
	bridgeName         = "eni-br0"
)

func init() {
	runtime.LockOSThread()
}

type NetConf struct {
	types.NetConf
}

func main() {
	defer log.Flush()
	logger.SetupLogger(logger.GetLogFileLocation(defaultLogFilePath))

	var printVersion bool
	flag.BoolVar(&printVersion, "version", false, "prints version and exits")
	flag.Parse()

	if printVersion {
		printVersionInfo()
		return
	}

	skel.PluginMain(add, del, cniversion.PluginSupports("0.3.0"))
}

func printVersionInfo() {
	versionInfo, err := version.String()
	if err != nil {
		log.Errorf("Error getting version string: %v", err)
		return
	}
	fmt.Println(versionInfo)
}

func add(args *skel.CmdArgs) error {
	// Get config
	var conf NetConf
	err := json.Unmarshal(args.StdinData, &conf)
	if err != nil {
		return err
	}

	// Create bridge if needed
	bridge, err := createBridge()
	if err != nil {
		return err
	}

	// Get net ns
	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return err
	}
	defer netns.Close()

	hostIfaceName := ""
	err = netns.Do(func(hostNS ns.NetNS) error {
		hostVeth, _, err := ip.SetupVeth(args.IfName, 1500, hostNS)
		if err != nil {
			return err
		}

		hostIfaceName = hostVeth.Attrs().Name
		return nil
	})

	if err != nil {
		return err
	}

	hostVeth, err := netlink.LinkByName(hostIfaceName)
	if err != nil {
		return err
	}

	err = netlink.LinkSetMaster(hostVeth, bridge)
	if err != nil {
		return err
	}

	return nil
}

func createBridge() (*netlink.Bridge, error) {
	la := netlink.NewLinkAttrs()
	la.Name = bridgeName

	bridge := &netlink.Bridge{la}
	err := netlink.LinkAdd(bridge)

	if err != nil && err != syscall.EEXIST {
		return nil, errors.Wrap(err, "error creating bridge")
	}

	bridgeLink, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return nil, errors.Wrap(err, "error looking up bridge device")
	}

	bridge, ok := bridgeLink.(*netlink.Bridge)
	if !ok {
		return nil, errors.Errorf("%s is not a bridge device", bridgeName)
	}

	err = netlink.LinkSetUp(bridge)
	if err != nil {
		return nil, errors.Wrap(err, "failed to enable bridge device")
	}

	return bridge, nil
}

func del(args *skel.CmdArgs) error {
	return nil
}
