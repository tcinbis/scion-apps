// Copyright 2021 ETH Zurich
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pan

import (
	"context"
	"fmt"
	"github.com/scionproto/scion/go/pkg/pathprobe"
	"math/rand"
	"net"
	"os"
	"sync"
	"time"

	"github.com/scionproto/scion/go/lib/addr"
	daemon "github.com/scionproto/scion/go/lib/sciond" // renamed upstream, daemon is new name
	"github.com/scionproto/scion/go/lib/snet"
	"github.com/scionproto/scion/go/lib/snet/addrutil"
	"github.com/scionproto/scion/go/lib/sock/reliable"
)

type scionHostContext struct {
	ia            addr.IA
	sciond        daemon.Connector
	dispatcher    reliable.Dispatcher
	hostInLocalAS net.IP
}

const (
	initTimeout = 1 * time.Second
)

var gScionHostContext scionHostContext
var initOnce sync.Once

// host initialises and returns the singleton default scionHostContext.
func host() *scionHostContext {
	initOnce.Do(mustInitScionHostContext)
	return &gScionHostContext
}

func LocalIA() addr.IA {
	return host().ia
}

func mustInitScionHostContext() {
	scionHostCtx, err := initScionHostContext()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing SCION host context: %v\n", err)
		os.Exit(1)
	}
	gScionHostContext = scionHostCtx
}

func initScionHostContext() (scionHostContext, error) {
	ctx, cancel := context.WithTimeout(context.Background(), initTimeout)
	defer cancel()
	dispatcher, err := findDispatcher()
	if err != nil {
		return scionHostContext{}, err
	}
	sciondConn, err := findSciond(ctx)
	if err != nil {
		return scionHostContext{}, err
	}
	localIA, err := sciondConn.LocalIA(ctx)
	if err != nil {
		return scionHostContext{}, err
	}
	hostInLocalAS, err := findAnyHostInLocalAS(ctx, sciondConn)
	if err != nil {
		return scionHostContext{}, err
	}
	return scionHostContext{
		ia:            localIA,
		sciond:        sciondConn,
		dispatcher:    dispatcher,
		hostInLocalAS: hostInLocalAS,
	}, nil
}

func findSciond(ctx context.Context) (daemon.Connector, error) {
	address, ok := os.LookupEnv("SCION_DAEMON_ADDRESS")
	if !ok {
		address = daemon.DefaultAPIAddress
	}
	sciondConn, err := daemon.NewService(address).Connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to SCIOND at %s (override with SCION_DAEMON_ADDRESS): %w", address, err)
	}
	return sciondConn, nil
}

func findDispatcher() (reliable.Dispatcher, error) {
	path, err := findDispatcherSocket()
	if err != nil {
		return nil, err
	}
	dispatcher := reliable.NewDispatcher(path)
	return dispatcher, nil
}

func findDispatcherSocket() (string, error) {
	path, ok := os.LookupEnv("SCION_DISPATCHER_SOCKET")
	if !ok {
		path = reliable.DefaultDispPath
	}
	err := statSocket(path)
	if err != nil {
		return "", fmt.Errorf("error looking for SCION dispatcher socket at %s (override with SCION_DISPATCHER_SOCKET): %w", path, err)
	}
	return path, nil
}

func statSocket(path string) error {
	fileinfo, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !isSocket(fileinfo.Mode()) {
		return fmt.Errorf("%s is not a socket (mode: %s)", path, fileinfo.Mode())
	}
	return nil
}

func isSocket(mode os.FileMode) bool {
	return mode&os.ModeSocket != 0
}

// findAnyHostInLocalAS returns the IP address of some (infrastructure) host in the local AS.
func findAnyHostInLocalAS(ctx context.Context, sciondConn daemon.Connector) (net.IP, error) {
	addr, err := daemon.TopoQuerier{Connector: sciondConn}.UnderlayAnycast(ctx, addr.SvcCS)
	if err != nil {
		return nil, err
	}
	return addr.IP, nil
}

// defaultLocalIP returns _a_ IP of this host in the local AS.
//
// The purpose of this function is to workaround not being able to bind to
// wildcard addresses in snet.
// See note on wildcard addresses in the package documentation.
func defaultLocalIP() (net.IP, error) {
	return addrutil.ResolveLocal(host().hostInLocalAS)
}

func defaultLocalAddr(local *net.UDPAddr) (*net.UDPAddr, error) {
	if local == nil {
		local = &net.UDPAddr{}
	}
	if local.IP == nil || local.IP.IsUnspecified() {
		localIP, err := defaultLocalIP()
		if err != nil {
			return nil, err
		}
		local = &net.UDPAddr{IP: localIP, Port: local.Port}
	}
	return local, nil
}

func (h *scionHostContext) queryPaths(ctx context.Context, dst addr.IA) ([]*Path, error) {
	flags := daemon.PathReqFlags{Refresh: false, Hidden: false}
	snetPaths, err := h.sciond.Paths(ctx, addr.IA(dst), addr.IA{}, flags)
	if err != nil {
		fmt.Println("Got error from sciond")
		return nil, err
	}
	if len(snetPaths) == 0 {
		fmt.Println("Got empty paths from sciond")
	}

	fmt.Printf("Received %d paths.\n", len(snetPaths))

	// probe status of paths
	// WARNING: This can higher level protocols cause to timeout if it is executed inside a Read or Write as it takes at most x seconds to probe path
	subCtx, cancelF := context.WithTimeout(ctx, 1*time.Second)
	defer cancelF()
	statuses, err := pathprobe.Prober{
		DstIA:   dst,
		LocalIA: h.ia,
		LocalIP: h.hostInLocalAS,
		ID:      uint16(rand.Uint32()),
	}.GetStatuses(subCtx, snetPaths)

	if err != nil {
		return nil, fmt.Errorf("Error probing paths: %v.", err)
	}

	paths := make([]*Path, 0, len(snetPaths))
	for _, p := range snetPaths {

		// TODO: Or should we be cautious and better skip the paths we have no status for?
		if status, ok := statuses[pathprobe.PathKey(p)]; ok && status.Status != pathprobe.StatusAlive {
			// if we have a status and it is NOT alive then skip the path
			continue
		}

		snetMetadata := p.Metadata()
		metadata := &PathMetadata{
			Interfaces:   convertPathInterfaceSlice(snetMetadata.Interfaces),
			MTU:          snetMetadata.MTU,
			Latency:      snetMetadata.Latency,
			Bandwidth:    snetMetadata.Bandwidth,
			Geo:          snetMetadata.Geo,
			LinkType:     snetMetadata.LinkType,
			InternalHops: snetMetadata.InternalHops,
			Notes:        snetMetadata.Notes,
		}
		paths = append(paths, &Path{
			Source:      h.ia,
			Destination: dst,
			Metadata:    metadata,
			Fingerprint: pathSequenceFromInterfaces(metadata.Interfaces).Fingerprint(),
			Expiry:      snetMetadata.Expiry,
			ForwardingPath: ForwardingPath{
				spath:    p.Path(),
				underlay: p.UnderlayNextHop(),
			},
		})
	}
	fmt.Printf("Got %d paths which are alive.\n", len(paths))
	return paths, nil
}

func convertPathInterfaceSlice(spis []snet.PathInterface) []PathInterface {
	pis := make([]PathInterface, len(spis))
	for i, spi := range spis {
		pis[i] = PathInterface{
			IA:   spi.IA,
			IfID: IfID(spi.ID),
		}
	}
	return pis
}
