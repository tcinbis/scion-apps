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
	"net"
	"sync"
	"time"
)

// DialPUDP creates a connected PUDP conn.
// TODO: Extended dial Config?
func DialPUDP(ctx context.Context, local *net.UDPAddr, remote UDPAddr, policy Policy) (net.Conn, error) {

	controller := &pudpController{}

	udpConn, err := DialUDP(ctx, local, remote, controller)
	if err != nil {
		return nil, err
	}

	controller.Run(udpConn.(*connectedConn))

	return &connectedPUDPConn{
		connectedConn: udpConn.(*connectedConn),
		controller:    controller,
	}, nil
}

type connectedPUDPConn struct {
	*connectedConn
	controller *pudpController
}

func (c *connectedPUDPConn) Write(b []byte) (int, error) {

	paths, header := c.controller.decide()
	msg := append(header, b...)
	for _, path := range paths {
		c.connectedConn.WritePath(path, msg)
	}
	return len(b), nil // XXX?
	/*
		// Note: the server side does not do any of this.

	*/
}

func (c *connectedPUDPConn) Read(b []byte) (int, error) {
	for {
		nr, path, err := c.connectedConn.ReadPath(b)
		if err != nil {
			return 0, err
		}
		v := &pudpControllerPacketVisitor{}
		err = pudpParseHeader(b[:nr], v)
		if err != nil {
			continue
		}
		err = c.controller.registerPacket(path, v.identifier, v.pongSequenceNum)
		if err != nil {
			continue
		}
		n := copy(b, v.pld)
		return n, nil
	}
}

func (c *connectedPUDPConn) Close() error {
	c.controller.Close()
	return c.connectedConn.Close()
}

type pudpController struct {
	mutex sync.Mutex

	// client side settings
	maxRace       int
	probeInterval time.Duration
	probeWindow   time.Duration

	/*
		// server side settings
		enablePong      bool
		enableRaceDedup bool
		enableIdentify  bool
		identifier      []IfID
	*/

	Policy     Policy
	unfiltered []*Path
	paths      []*Path
	current    *Path

	requestedRemoteIdentifier bool
	remoteIdentifier          []IfID

	raceSequenceNum uint16
	pingSequenceNum uint16
	pingTime        time.Time

	stop chan struct{}
}

func (c *pudpController) Path() (*Path, error) {
	panic("not implemented") // We only call connectedConn.WritePath(), bypassing calls to this func
}

func (c *pudpController) decide() ([]*Path, []byte) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	header := pudpHeaderBuilder{} // TODO use some buffer somewhere
	paths := []*Path{c.current}
	if c.current == nil {
		// no path selected (yet), we're racing
		paths = c.paths
		if len(paths) > c.maxRace {
			paths = paths[:c.maxRace]
		}
		if len(paths) > 1 {
			header.race(c.raceSequenceNum)
			c.raceSequenceNum++
		}
		// send one ping during racing
		if (c.pingTime == time.Time{}) {
			c.pingTime = time.Now() // TODO:
			header.ping(c.pingSequenceNum)
		}
	} else {

	}
	if c.remoteIdentifier == nil {
		header.identify()
	}
	/*if c.pingCurrent() {
		header.ping(c.controller.pingSequenceNum())
	}*/
	return paths, header.buf.Bytes()
}

func (c *pudpController) SetPaths(paths []*Path) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.unfiltered = paths
	c.paths = c.filterPaths(paths)
}

func (c *pudpController) OnPathDown(path *Path, pi PathInterface) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	stats.notifyDown(path.Fingerprint, pi)

	if c.current != nil &&
		(isInterfaceOnPath(c.current, pi) || path.Fingerprint == c.current.Fingerprint) {
		//	fmt.Println("failover:", s.current, len(s.paths))
	}
}

func (c *pudpController) Run(udpConn *connectedConn) {
	probeTimer := time.NewTimer(0)
	<-probeTimer.C
	inProbeWindow := false
	for {
		select {
		case <-c.stop:
			break
		case <-probeTimer.C:
			if !inProbeWindow {
				// start probe interval
				probeTimer.Reset(c.probeWindow)
				inProbeWindow = false
			} else {
				// probe window ended
				probeTimer.Reset(c.probeInterval - c.probeWindow)
				inProbeWindow = true
			}
		}
	}
}

func (c *pudpController) Close() error {
	c.stop <- struct{}{}
	return nil
}

func (c *pudpController) filterPaths(paths []*Path) []*Path {
	paths = filterPathsByLastHopInterface(paths, c.remoteIdentifier)
	if c.Policy != nil {
		return c.Policy.Filter(paths)
	}
	return paths
}

func (c *pudpController) registerPacket(path *Path, identifier []IfID,
	pongSequenceNum interface{}) error {

	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Register first remote identifier; effectively, pick an anycast instance
	if c.remoteIdentifier == nil && identifier != nil {
		c.remoteIdentifier = identifier
		c.paths = c.filterPaths(c.unfiltered)
	} else if c.remoteIdentifier != nil && identifier != nil {
		// if we've previously seen a remote identifier, drop the packet if this
		// does not match (response from a different anycast instance)
		// Simple comparison seems ok here, if it's the same instance there's no reason it should
		// e.g. change the order or the entries.
		if !ifidSliceEqual(c.remoteIdentifier, identifier) {
			return errors.New("unexpected non-matching remote identifier")
		}
	}

	// If we are awaiting the first reply packet during/after racing, pick this
	// path to continue sending on (if it is indeed in the set of available
	// paths...).
	if c.current == nil {
		for _, p := range c.paths {
			if p.Fingerprint == path.Fingerprint {
				c.current = p
				break
			}
		}
	}

	if pongSequenceNum != nil {
		c.registerPong(pongSequenceNum.(int), path)
	}
	return nil
}

func (c *pudpController) registerPong(seq int, path *Path) {
	if c.pingSequenceNum == seq {
		// XXX: we should register the samples already when
		// sending the probe and then update it on the pong. This will give a
		// better view of dead paths when sorting
		stats.registerLatency(path.Fingerprint, time.Since(c.pingTime))
	}
}

func filterPathsByLastHopInterface(paths []*Path, interfaces []IfID) []*Path {
	// empty interface list means don't care
	if len(interfaces) == 0 {
		return paths
	}
	filtered := make([]*Path, 0, len(paths))
	for _, p := range paths {
		if p.Metadata != nil || len(p.Metadata.Interfaces) > 0 {
			last := p.Metadata.Interfaces[len(p.Metadata.Interfaces)-1]
			// if last in list, keep it:
			for _, ifid := range interfaces {
				if last.IfID == ifid {
					filtered = append(filtered, p)
					break
				}
			}
		}
	}
	return filtered
}

func ifidSliceEqual(a, b []IfID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type pudpControllerPacketVisitor struct {
	pld             []byte
	identifier      []IfID
	pongSequenceNum interface{} // optional int
}

func (v *pudpControllerPacketVisitor) payload(b []byte) {
	v.pld = b
}

func (v *pudpControllerPacketVisitor) race(seq uint16) {
	// ignore (or error!?)
}

func (v *pudpControllerPacketVisitor) ping(seq uint16) {
	// ignore (or error!?)
}

func (v *pudpControllerPacketVisitor) pong(seq uint16) {
	v.pongSequenceNum = int(seq)
}

func (v *pudpControllerPacketVisitor) identify() {
	// ignore (or error!?)
}

func (v *pudpControllerPacketVisitor) me(ifids []IfID) {
	v.identifier = ifids
}
