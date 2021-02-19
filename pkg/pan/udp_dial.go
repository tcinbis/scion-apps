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
)

var errNoPath error = errors.New("no path")

// Selector (or Router, Pather, Scheduler?) is owned by a single **connected** socket. Stateful.
// The Path() function is invoked for every single packet.
type Selector interface {
	Path() (*Path, error)
	SetPaths([]*Path)
	OnPathDown(*Path, PathInterface)
}

// XXX: should policy be part of the selector? would generalize things a bit.
// Mostly the same thing, just move the policy evaluation into SetPaths. But
// it's a bit awkward because of the local/remote addresses inputs to the
// policy...
// It would make "SetPolicy" a bit easier though..
// Perhaps expose the subscribe interface? ;/
func DialUDP(ctx context.Context, local *net.UDPAddr, remote UDPAddr, policy Policy, selector Selector) (net.Conn, error) {

	local, err := defaultLocalAddr(local)
	if err != nil {
		return nil, err
	}

	if selector == nil {
		selector = &DefaultSelector{}
	}

	raw, slocal, err := openScionPacketConn(ctx, local, selector)
	if err != nil {
		return nil, err
	}
	// XXX: dont do this for dst in local IA!
	subscriber, err := openPolicySubscriber(ctx, policy, slocal, remote, selector)
	if err != nil {
		return nil, err
	}
	return &connectedConn{
		scionUDPConn: scionUDPConn{
			raw: raw,
		},
		local:      slocal,
		remote:     remote,
		subscriber: subscriber,
		selector:   selector,
	}, nil
}

type connectedConn struct {
	scionUDPConn

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

func (c *connectedConn) Write(b []byte) (int, error) {
	path, err := c.selector.Path()
	if err != nil {
		return 0, err
	}
	return c.scionUDPConn.writeMsg(c.local, c.remote, path, b)
}

func (c *connectedConn) Read(b []byte) (int, error) {
	for {
		n, remote, _, err := c.scionUDPConn.readMsg(b)
		if err != nil {
			return n, err
		}
		if !remote.Equal(c.remote) {
			continue // connected! Ignore spurious packets from wrong source
		}
		return n, err
	}
}

func (c *connectedConn) Close() error {
	_ = c.subscriber.Close()
	return c.scionUDPConn.Close()
}

//////////////////// subscriber

// enterprise path setter
type pathSetter interface {
	SetPaths([]*Path)
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

func (s *policySubscriber) refresh(dst IA, paths []*Path) {
	s.setFilteredPaths(paths)
}

func (s *policySubscriber) setFilteredPaths(paths []*Path) {
	if s.policy != nil {
		paths = s.policy.Filter(paths, s.local, s.remote)
	}
	for _, p := range paths {
		fmt.Println(p)
	}
	s.target.SetPaths(paths)
}

//////////////////// selector

var _ Selector = &DefaultSelector{}

type DefaultSelector struct {
	paths              []*Path
	current            int
	currentFingerprint pathFingerprint
}

func (s *DefaultSelector) Path() (*Path, error) {
	if len(s.paths) == 0 {
		return nil, errNoPath
	}
	return s.paths[s.current], nil
}

func (s *DefaultSelector) SetPaths(paths []*Path) {
	curr := 0
	if s.currentFingerprint != "" {
		for i, p := range paths {
			if p.Fingerprint == s.currentFingerprint {
				curr = i
				break
			}
		}
	}
	s.paths = paths
	s.current = curr
	if len(s.paths) > 0 {
		s.currentFingerprint = s.paths[s.current].Fingerprint
	}
}

func (s *DefaultSelector) OnPathDown(path *Path, pi PathInterface) {
	if isInterfaceOnPath(s.paths[s.current], pi) || path.Fingerprint == s.currentFingerprint {
		// XXX: this is a quite dumb; will forget about the down notifications immediately.
		// XXX: this should be replaced with sending this to "Stats DB". Then the
		// selector needs to be subscribed to the stats DB.

		// Try next path. Note that this will keep cycling through all paths if none are working.
		s.current = (s.current + 1) % len(s.paths)
		fmt.Println("failover:", s.current, len(s.paths))
		s.currentFingerprint = s.paths[s.current].Fingerprint
	}
}
