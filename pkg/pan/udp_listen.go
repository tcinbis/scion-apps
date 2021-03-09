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

/*

- never lookup paths during sending a packet
- assumption: listener only ever replies. Seems like an ok restriction to make.
	We could have "proper" unconnected conn, i.e. one that allows sending to
	anyone, by combining this with the Dial logic, but things get more
	complicated (is it worth keeping these paths up to date? etc...)
- Observation:
	- path oblivient upper layers cannot just keep using same path forever (snet/squic approach)
	- a path aware application can use path-y API to explicitly reply without the reply path recording overhead
*/

var errBadDstAddress error = errors.New("dst address not a UDPAddr")

type UnconnectedSelector interface {
	ReplyPath(src, dst UDPAddr) *Path
	OnPacketReceived(src, dst UDPAddr, path *Path)
	OnPathDown(*Path, PathInterface)
}

func ListenUDP(ctx context.Context, local *net.UDPAddr,
	selector UnconnectedSelector) (net.PacketConn, error) {

	local, err := defaultLocalAddr(local)
	if err != nil {
		return nil, err
	}

	if selector == nil {
		selector = NewDefaultReplySelector()
	}
	raw, slocal, err := openScionPacketConn(ctx, local, selector)
	if err != nil {
		return nil, err
	}
	return &unconnectedConn{
		scionUDPConn: scionUDPConn{
			raw: raw,
		},
		local:    slocal,
		selector: selector,
	}, nil
}

type unconnectedConn struct {
	scionUDPConn

	local    UDPAddr
	selector UnconnectedSelector
}

func (c *unconnectedConn) LocalAddr() net.Addr {
	return c.local
}

func (c *unconnectedConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, remote, path, err := c.ReadFromPath(b)
	c.selector.OnPacketReceived(remote, c.local, path)
	return n, remote, err
}

// XXX: expose or remove this? :/
func (c *unconnectedConn) ReadFromPath(b []byte) (int, UDPAddr, *Path, error) {
	n, remote, fwPath, err := c.scionUDPConn.readMsg(b)
	if err != nil {
		return n, UDPAddr{}, nil, err
	}
	path, err := reversePathFromForwardingPath(remote.IA, c.local.IA, fwPath)
	return n, remote, path, err
}

func (c *unconnectedConn) WriteTo(b []byte, dst net.Addr) (int, error) {
	sdst, ok := dst.(UDPAddr)
	if !ok {
		return 0, errBadDstAddress
	}
	var path *Path
	if c.local.IA != sdst.IA {
		path = c.selector.ReplyPath(c.local, sdst)
		if path == nil {
			return 0, errNoPath
		}
	}
	return c.WriteToPath(b, sdst, path)
}

func (c *unconnectedConn) WriteToPath(b []byte, dst UDPAddr, path *Path) (int, error) {
	return c.scionUDPConn.writeMsg(c.local, dst, path, b)
}

func (c *unconnectedConn) Close() error {
	return c.scionUDPConn.Close()
}

var _ UnconnectedSelector = &DefaultReplySelector{}

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
	replyPath map[udpAddrKey][]replyPathEntry
}

type replyPathEntry struct {
	path *Path
	seen time.Time
}

func NewDefaultReplySelector() *DefaultReplySelector {
	return &DefaultReplySelector{
		replyPath: make(map[udpAddrKey][]replyPathEntry),
	}
}

func (s *DefaultReplySelector) ReplyPath(src, dst UDPAddr) *Path {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	paths, ok := s.replyPath[makeKey(dst)]
	if !ok || len(paths) == 0 {
		return nil
	}
	mostRecent := 0
	for i := 1; i < len(paths); i++ {
		// TODO check stats DB
		if paths[i].seen.After(paths[mostRecent].seen) {
			mostRecent = i
		}
	}
	return paths[mostRecent].path
}

func (s *DefaultReplySelector) OnPacketReceived(src, dst UDPAddr, path *Path) {
	if path == nil {
		return
	}

	s.mtx.Lock()
	defer s.mtx.Unlock()

	ksrc := makeKey(src)
	paths := s.replyPath[ksrc]
	for _, e := range paths {
		if e.path.Fingerprint == path.Fingerprint {
			e.path = path
			e.seen = time.Now()
			return
		}
	}
	s.replyPath[ksrc] = append(paths, replyPathEntry{
		path: path,
		seen: time.Now(),
	})
}

func (s *DefaultReplySelector) OnPathDown(*Path, PathInterface) {
	// TODO: report to stats DB
}
