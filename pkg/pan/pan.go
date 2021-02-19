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

// pan, pan ready, pan fried, Path Aware Networking, peter pan,
/*
names:
	supa
	sap
	scope
	riddims:
	ship:
		helm,	rudder, pilot, scout, spy, till, tiller, skipper
		foghorn



*/
/*
features / usecases

- select path based on filter and preference
	- filter based on ISD / ASes traversed
	- filter based on attributes (length, latency, ...)
	- order by attributes
	- disjoint from previous paths
- interactive choice
	- optionally with fallback in case path dies
		-> in this mode, manual input can be considered a preference order
- keep paths fresh
  - reevaluate selection or just update selected path?
		-> answer: reevaluate selection; partial order, compare from current
							 only update selected should be achievable too (analogous to interactive choice)
- remove dead paths and fail over
	- by SCMP
	- by indication of application
	- by expiration in less than ~10s

- race opening
- path negotiation
- server side path policy?
- active probing
	-> in data stream? out of stream? or only on the side, control?
- active path control from outside (API/user interaction -- see below)

- couple multiple things to use disjoint paths to maximize bandwidth
	-> handle fallbacks ooof


- http/quic with path control
	- application can give policy
	- application can change policy
	- application can somehow determine currently used path (ok if this is not part of "stable" API)
	- application can change currently used path
	- in a UI like some browser extension, a user may e.g.
		- see the currently used path, see the dial race and path failover
		- explicitly forbid/unforbid a specific path, switch away if it's the current path
		- force use of a specific path


Note: how to handle MTU?

*/

package pan

import (
	"fmt"
	"net"

	"github.com/scionproto/scion/go/lib/addr"
	"github.com/scionproto/scion/go/lib/snet"
)

// XXX: leaking addr
type IA = addr.IA
type IfID = uint64

// NOTE: does _NOT_ contain path
type UDPAddr struct {
	IA   IA
	IP   net.IP
	Port int
}

func (a UDPAddr) Network() string {
	return "scion+udp"
}

func (a UDPAddr) String() string {
	// XXX: maybe we can start to experiment with different representations here.
	// I like
	//   isd-as-ipv4:port
	//   [isd-as-ipv6]:port (who cares about zones anyway?)
	if a.IP.To4() == nil {
		return fmt.Sprintf("%s,[%s]:%d", a.IA, a.IP, a.Port) // XXX: use snet stuff?
	} else {
		return fmt.Sprintf("%s,%s:%d", a.IA, a.IP, a.Port) // XXX: use snet stuff?
	}
}

func (a UDPAddr) Equal(x UDPAddr) bool {
	return a.IA == x.IA &&
		a.IP.Equal(x.IP) &&
		a.Port == x.Port
}

func ParseUDPAddr(s string) (UDPAddr, error) {
	addr, err := snet.ParseUDPAddr(s)
	if err != nil {
		return UDPAddr{}, err
	}
	return UDPAddr{
		IA:   addr.IA,
		IP:   addr.Host.IP,
		Port: addr.Host.Port,
	}, nil
}
