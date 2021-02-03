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
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/scionproto/scion/go/lib/addr"
	"github.com/scionproto/scion/go/lib/slayers"
	"github.com/scionproto/scion/go/lib/snet"
)

var errNoPath error = errors.New("no path")

// Policy is a stateless filter / sorter for paths.
type Policy interface {
	// Filter and prioritize paths
	Filter(paths []Path, local, remote UDPAddr) []Path
}

// Selector (or Router, Pather, Scheduler?) is owned by a single **connected** socket. Stateful.
// The Path() function is invoked for every single packet.
// This should
type Selector interface {
	Path() (Path, error)
	SetPaths([]Path)
	OnPathDown(Path, PathInterface)
}

// XXX: should policy be part of the selector? would generalize things a bit. Mostly the same thing, just move the policy evaluation into SetPaths. But it's a bit awkward because of the local/remote addresses inputs to the policy...
func DialUDP(ctx context.Context, local *net.UDPAddr, remote UDPAddr, policy Policy, selector Selector) (net.Conn, error) {
	// XXX: open connection

	if local == nil {
		local = &net.UDPAddr{}
	}
	if local.IP == nil || local.IP.IsUnspecified() {
		localIP, err := defaultLocalIP()
		if err != nil {
			return nil, err
		}
		local = &net.UDPAddr{IP: localIP, Port: local.Port, Zone: local.Zone}
	}

	if selector == nil {
		selector = &DefaultSelector{}
	}

	scmpHandler := &selectorSCMPHandler{
		selector: selector,
	}
	raw, slocal, err := openRawSCIONSocket(ctx, local, scmpHandler)
	if err != nil {
		return nil, err
	}
	subscriber, err := openPolicySubscriber(ctx, policy, slocal, remote, selector)
	if err != nil {
		return nil, err
	}
	return &connectedConn{
		raw:        raw,
		local:      slocal,
		remote:     remote,
		subscriber: subscriber,
		selector:   selector,
	}, nil
}

type connectedConn struct {
	raw         snet.PacketConn
	readMutex   sync.Mutex
	readBuffer  snet.Bytes
	writeMutex  sync.Mutex
	writeBuffer snet.Bytes

	local      UDPAddr
	remote     UDPAddr
	subscriber *policySubscriber
	selector   Selector
}

func (c *connectedConn) LocalAddr() net.Addr {
	return c.local
}

func (c *connectedConn) RemoteAddr() net.Addr {
	return c.remote
}

func (c *connectedConn) SetDeadline(t time.Time) error {
	return c.raw.SetDeadline(t)
}

func (c *connectedConn) SetReadDeadline(t time.Time) error {
	return c.raw.SetReadDeadline(t)
}

func (c *connectedConn) SetWriteDeadline(t time.Time) error {
	return c.raw.SetWriteDeadline(t)
}

func (c *connectedConn) Write(b []byte) (int, error) {
	path, err := c.selector.Path()
	if err != nil {
		return 0, err
	}

	// ugh ugh ugh
	pkt := &snet.Packet{
		Bytes: c.writeBuffer,
		PacketInfo: snet.PacketInfo{ // bah
			Source: snet.SCIONAddress{
				IA:   c.local.IA,
				Host: addr.HostFromIP(c.local.IP),
			},
			Destination: snet.SCIONAddress{
				IA:   c.remote.IA,
				Host: addr.HostFromIP(c.remote.IP),
			},
			Path: path.Path(),
			Payload: snet.UDPPayload{
				SrcPort: uint16(c.local.Port),
				DstPort: uint16(c.remote.Port),
				Payload: b,
			},
		},
	}
	c.writeMutex.Lock()
	defer c.writeMutex.Unlock()
	err = c.raw.WriteTo(pkt, path.UnderlayNextHop()) // gnaaah
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

func (c *connectedConn) Read(b []byte) (int, error) {
	c.readMutex.Lock()
	pkt := snet.Packet{
		Bytes: c.readBuffer,
	}
	for { // XXX: deadline?
		// Note: SCMP handler called from here!
		var lastHop net.UDPAddr
		err := c.raw.ReadFrom(&pkt, &lastHop)
		if err != nil {
			return 0, err
		}
		udp, ok := pkt.Payload.(snet.UDPPayload)
		if !ok {
			continue // ignore non-UDP packet
		}
		if pkt.Source.IA != c.remote.IA ||
			!pkt.Source.Host.IP().Equal(c.remote.IP) ||
			int(udp.SrcPort) != c.remote.Port {
			continue // connected conn; ignore packet from wrong source
		}
		n := copy(b, udp.Payload)
		return n, nil
	}
}

func (c *connectedConn) Close() error {
	_ = c.subscriber.Close()
	return c.raw.Close()
}

// XXX: this should be replace with sending the path down notification to the stats-DB
type selectorSCMPHandler struct {
	selector Selector
}

func (h *selectorSCMPHandler) Handle(pkt *snet.Packet) error {
	scmp := pkt.Payload.(snet.SCMPPayload)
	switch scmp.Type() {
	case slayers.SCMPTypeExternalInterfaceDown:
		msg := pkt.Payload.(snet.SCMPExternalInterfaceDown)
		pi := PathInterface{
			IA:   msg.IA,
			IfID: msg.Interface,
		}
		h.selector.OnPathDown(nil, pi) // XXX: partialPath!!
		return nil
	case slayers.SCMPTypeInternalConnectivityDown:
		msg := pkt.Payload.(snet.SCMPInternalConnectivityDown)
		pi := PathInterface{
			IA:   msg.IA,
			IfID: msg.Egress,
		}
		h.selector.OnPathDown(nil, pi) // XXX: partialPath!!
		return nil
	default:
		// TODO: OpError for other SCMPs!
		return nil
	}
}

//////////////////// subscriber

// enterprise path setter
type pathSetter interface {
	SetPaths([]Path)
}

type policySubscriber struct {
	policy Policy
	local  UDPAddr
	remote UDPAddr
	target pathSetter
}

func openPolicySubscriber(ctx context.Context, policy Policy, local, remote UDPAddr, target pathSetter) (*policySubscriber, error) {
	s := &policySubscriber{
		policy: policy,
		local:  local,
		remote: remote,
		target: target,
	}
	paths, err := pool.subscribe(ctx, remote.IA, s)
	if err != nil {
		return nil, nil
	}
	s.setFilteredPaths(paths)
	return s, nil
}

func (s *policySubscriber) Close() error {
	if s != nil {
		pool.unsubscribe(s.remote.IA, s)
	}
	return nil
}

func (s *policySubscriber) refresh(dst IA, paths []Path) {
	s.setFilteredPaths(paths)
}

func (s *policySubscriber) setFilteredPaths(paths []Path) {
	if s.policy != nil {
		paths = s.policy.Filter(paths, s.local, s.remote)
	}
	for _, p := range paths {
		fmt.Println(p)
	}
	s.target.SetPaths(paths)
}

///////////////// policies
type Pinned struct {
	sequence []pathFingerprint
}

func (p *Pinned) Filter(paths []Path, src, dst UDPAddr) []Path {
	filtered := make([]Path, 0, len(p.sequence))
	for _, s := range p.sequence {
		for _, p := range paths {
			if fingerprint(p) == s {
				filtered = append(filtered, p)
				break
			}
		}
	}
	return filtered
}

type Preferred struct {
	sequence []pathFingerprint
}

func (p *Preferred) Filter(paths []Path, src, dst UDPAddr) []Path {
	filtered := make([]Path, 0, len(p.sequence))
	for _, s := range p.sequence {
		for _, p := range paths {
			if fingerprint(p) == s {
				filtered = append(filtered, p)
				break
			}
		}
	}
	return filtered
}

//////////////////// selector

var _ Selector = &DefaultSelector{}

type DefaultSelector struct {
	paths              []Path
	current            int
	currentFingerprint pathFingerprint
}

func (s *DefaultSelector) Path() (Path, error) {
	if len(s.paths) == 0 {
		return nil, errNoPath
	}
	return s.paths[s.current], nil
}

func (s *DefaultSelector) SetPaths(paths []Path) {
	curr := 0
	if s.currentFingerprint != "" {
		fmt.Println("looking for prev", s.currentFingerprint)
		for i, p := range paths {
			if fingerprint(p) == s.currentFingerprint {
				curr = i
				fmt.Println("found prev", i)
				break
			}
		}
	}
	s.paths = paths
	s.current = curr
	if len(s.paths) > 0 {
		s.currentFingerprint = fingerprint(s.paths[s.current])
	}
}

func (s *DefaultSelector) OnPathDown(path Path, pi PathInterface) {
	if isInterfaceOnPath(s.paths[s.current], pi) || fingerprint(path) == s.currentFingerprint {
		// XXX: this is a bit dumb, forgets about the down notifications. Need the stats DB
		// for this. Then the selector needs to be subscribed to the stats DB.

		// Try next path. Note that this will keep cycling through all paths if none are working.
		s.current = (s.current + 1) % len(s.paths)
		fmt.Println(s.current, len(s.paths))
		s.currentFingerprint = fingerprint(s.paths[s.current])
	}
}
