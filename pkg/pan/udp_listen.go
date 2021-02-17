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
)

var errBadDstAddress error = errors.New("dst address not a UDPAddr")

type ReplySelector interface {
	ReplyPath(src, dst UDPAddr) (Path, error)
	OnPacketReceived(src, dst UDPAddr, path Path)
	OnPathDown(Path, PathInterface)
}

func ListenUDP(ctx context.Context, local *net.UDPAddr,
	selector ReplySelector) (net.PacketConn, error) {

	err := defaultLocalAddr(local)
	if err != nil {
		return nil, err
	}

	raw, slocal, err := openScionPacketConn(ctx, local, selector)
	if err != nil {
		return nil, err
	}
	return &unconnectedConn{
		scionUDPConn: scionUDPConn{
			raw: raw,
		},
		local: slocal,
	}, nil
}

type unconnectedConn struct {
	scionUDPConn

	local    UDPAddr
	selector ReplySelector
}

func (c *unconnectedConn) LocalAddr() net.Addr {
	return c.local
}

func (c *unconnectedConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, remote, err := c.scionUDPConn.readMsg(b)
	if err != nil {
		return n, nil, err
	}
	c.selector.OnPacketReceived(remote, c.local, nil)
	return n, remote, err
}

func (c *unconnectedConn) WriteTo(b []byte, dst net.Addr) (int, error) {
	sdst, ok := dst.(UDPAddr)
	if !ok {
		return 0, errBadDstAddress
	}
	path, err := c.selector.ReplyPath(c.local, sdst)
	if err != nil {
		return 0, err
	}
	return c.scionUDPConn.writeMsg(c.local, sdst, path, b)
}

func (c *unconnectedConn) Close() error {
	return c.scionUDPConn.Close()
}

var _ ReplySelector = &DefaultReplySelector{}

type udpAddrKey struct {
	IA   IA
	IP   [16]byte
	Port int
}

func makeKey(a UDPAddr) udpAddrKey {
	k := udpAddrKey{
		IA:   a.IA,
		Port: a.Port,
	}
	copy(k.IP[:], a.IP.To16())
	return k
}

type DefaultReplySelector struct {
	mtx       sync.RWMutex
	replyPath map[udpAddrKey][]Path
}

func (s *DefaultReplySelector) ReplyPath(src, dst UDPAddr) (Path, error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	paths, ok := s.replyPath[makeKey(dst)]
	if !ok || len(paths) == 0 {
		return nil, errNoPath
	}
	return paths[0], nil
}

func (s *DefaultReplySelector) OnPacketReceived(src, dst UDPAddr, path Path) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	paths := s.replyPath[makeKey(src)]
	for _, p := range paths {
		if fingerprint(p) == fingerprint(path) {

		}
	}
}

func (s *DefaultReplySelector) OnPathDown(Path, PathInterface) {}
