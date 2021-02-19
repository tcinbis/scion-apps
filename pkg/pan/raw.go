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
	"net"
	"sync"
	"time"

	"github.com/scionproto/scion/go/lib/addr"
	"github.com/scionproto/scion/go/lib/slayers"
	"github.com/scionproto/scion/go/lib/snet"
	"github.com/scionproto/scion/go/lib/topology/underlay"
)

type pathDownHandler interface {
	OnPathDown(*Path, PathInterface)
}

// openScionPacketConn opens new raw SCION UDP conn.
// XXX: I'd prefer to make this onPathDown more explicit, but currently it
// _must_ be in the SCMP handler it seems.
func openScionPacketConn(ctx context.Context, local *net.UDPAddr,
	onPathDown pathDownHandler) (snet.PacketConn, UDPAddr, error) {

	dispatcher := host().dispatcher
	ia := host().ia

	rconn, port, err := dispatcher.Register(ctx, ia, local, addr.SvcNone)
	if err != nil {
		return nil, UDPAddr{}, err
	}

	conn := snet.NewSCIONPacketConn(rconn, &pathDownSCMPHandler{onPathDown}, true)
	slocal := UDPAddr{
		IA:   ia,
		IP:   local.IP,
		Port: int(port),
	}
	return conn, slocal, nil
}

// scionUDPConn contains the shared read and write code for different connection interfaces.
// Currently this just wraps snet.SCIONPacketConn.
type scionUDPConn struct {
	raw         snet.PacketConn
	readMutex   sync.Mutex
	readBuffer  snet.Bytes
	writeMutex  sync.Mutex
	writeBuffer snet.Bytes
}

func (c *scionUDPConn) SetDeadline(t time.Time) error {
	return c.raw.SetDeadline(t)
}

func (c *scionUDPConn) SetReadDeadline(t time.Time) error {
	return c.raw.SetReadDeadline(t)
}

func (c *scionUDPConn) SetWriteDeadline(t time.Time) error {
	return c.raw.SetWriteDeadline(t)
}

func (c *scionUDPConn) writeMsg(src, dst UDPAddr, path *Path, b []byte) (int, error) {
	pkt := &snet.Packet{
		Bytes: c.writeBuffer,
		PacketInfo: snet.PacketInfo{ // bah
			Source: snet.SCIONAddress{
				IA:   src.IA,
				Host: addr.HostFromIP(src.IP),
			},
			Destination: snet.SCIONAddress{
				IA:   dst.IA,
				Host: addr.HostFromIP(dst.IP),
			},
			Path: path.ForwardingPath.spath,
			Payload: snet.UDPPayload{
				SrcPort: uint16(src.Port),
				DstPort: uint16(dst.Port),
				Payload: b,
			},
		},
	}

	var nextHop *net.UDPAddr
	if src.IA == dst.IA {
		nextHop = &net.UDPAddr{
			IP:   dst.IP,
			Port: underlay.EndhostPort,
		}
	} else {
		// XXX: could have global lookup table with ifID->UDP instead of passing this around.
		// Might also allow to "properly" bind to wildcard (cache correct source address per ifID).
		nextHop = path.ForwardingPath.underlay
	}

	c.writeMutex.Lock()
	defer c.writeMutex.Unlock()
	err := c.raw.WriteTo(pkt, nextHop)
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

// readPkt is a helper for reading a single packet.
// Internally invokes the configured SCMP handler.
// Ignores non-UDP packets.
// Returns
func (c *scionUDPConn) readMsg(b []byte) (int, UDPAddr, ForwardingPath, error) {
	c.readMutex.Lock()
	defer c.readMutex.Unlock()
	for {
		pkt := snet.Packet{
			Bytes: c.readBuffer,
		}
		var lastHop net.UDPAddr
		err := c.raw.ReadFrom(&pkt, &lastHop)
		if err != nil {
			return 0, UDPAddr{}, ForwardingPath{}, err
		}
		udp, ok := pkt.Payload.(snet.UDPPayload)
		if !ok {
			continue // ignore non-UDP packet
		}
		remote := UDPAddr{
			IA:   pkt.Source.IA,
			IP:   append(net.IP{}, pkt.Source.Host.IP()...),
			Port: int(udp.SrcPort),
		}
		fw := ForwardingPath{
			spath:    pkt.Path.Copy(),
			underlay: &lastHop,
		}
		n := copy(b, udp.Payload)
		return n, remote, fw, nil
	}
}

func (c *scionUDPConn) Close() error {
	return c.raw.Close()
}

type pathDownSCMPHandler struct {
	pathDownHandler pathDownHandler
}

func (h *pathDownSCMPHandler) Handle(pkt *snet.Packet) error {
	scmp := pkt.Payload.(snet.SCMPPayload)
	switch scmp.Type() {
	case slayers.SCMPTypeExternalInterfaceDown:
		msg := pkt.Payload.(snet.SCMPExternalInterfaceDown)
		pi := PathInterface{
			IA:   msg.IA,
			IfID: msg.Interface,
		}
		h.pathDownHandler.OnPathDown(nil, pi) // XXX: forwarding path!!
		return nil
	case slayers.SCMPTypeInternalConnectivityDown:
		msg := pkt.Payload.(snet.SCMPInternalConnectivityDown)
		pi := PathInterface{
			IA:   msg.IA,
			IfID: msg.Egress,
		}
		h.pathDownHandler.OnPathDown(nil, pi) // XXX: forwarding path!!
		return nil
	default:
		// TODO: OpError for other SCMPs!
		return nil
	}
}
