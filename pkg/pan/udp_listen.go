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

// ReplySelector selects the reply path for WriteTo in a listener.
type ReplySelector interface {
	ReplyPath(src, dst UDPAddr) *Path
	OnPacketReceived(src, dst UDPAddr, path *Path)
	OnPathDown(PathFingerprint, PathInterface)
	Close() error
}

type UDPListener interface {
	net.PacketConn

	ReadFromPath(b []byte) (int, UDPAddr, *Path, error)
	MakeConnectionToRemote(ctx context.Context, remote UDPAddr, policy Policy, selector Selector) (Conn, error)
}

func ListenUDP(ctx context.Context, local *net.UDPAddr,
	selector ReplySelector) (UDPListener, error) {

	local, err := defaultLocalAddr(local)
	if err != nil {
		return nil, err
	}

	if selector == nil {
		selector = NewDefaultReplySelector()
	}
	stats.subscribe(selector)
	raw, slocal, err := openBaseUDPConn(ctx, local)
	if err != nil {
		return nil, err
	}
	return &listener{
		baseUDPConn: baseUDPConn{
			raw: raw,
		},
		local:    slocal,
		selector: selector,
	}, nil
}

type listener struct {
	baseUDPConn

	local    UDPAddr
	selector ReplySelector
}

func (c* listener) MakeConnectionToRemote(ctx context.Context, remote UDPAddr, policy Policy, selector Selector) (Conn, error) {
	if selector == nil {
		selector = &DefaultSelector{}
	}

	// If selector is not already populated with a path give it the reply path that we have
	if selector.Path() == nil {
		selector.SetPaths([]*Path {c.selector.ReplyPath(c.local, remote)})
	}

	var subscriber *pathRefreshSubscriber
	if remote.IA != c.local.IA {
		var err error
		// TODO: This is blocking. We already have at least one path and it would be nice to at least have the option to do a soft upgrade to full path awareness. I.e. let the connection start out with just the existing return path(s), do a full path query in the background and "upgrade" to full path control once sciond returns all the paths to the destination
		subscriber, err = openPathRefreshSubscriber(ctx, remote, policy, selector)
		if err != nil {
			return nil, err
		}
	}

	return &connection{
		baseUDPConn: &c.baseUDPConn,
		isListener: true,
		local:      c.local,
		remote:     remote,
		subscriber: subscriber,
		Selector:   selector,
	}, nil
}

func (c *listener) LocalAddr() net.Addr {
	return c.local
}

func (c *listener) ReadFrom(b []byte) (int, net.Addr, error) {
	n, remote, _, err := c.ReadFromPath(b)
	return n, remote, err
}

func (c *listener) ReadFromPath(b []byte) (int, UDPAddr, *Path, error) {
	n, remote, fwPath, err := c.baseUDPConn.readMsg(b)
	if err != nil {
		return n, UDPAddr{}, nil, err
	}
	path, err := reversePathFromForwardingPath(remote.IA, c.local.IA, fwPath)
	c.selector.OnPacketReceived(remote, c.local, path)
	return n, remote, path, err
}

func (c *listener) WriteTo(b []byte, dst net.Addr) (int, error) {
	sdst, ok := dst.(UDPAddr)
	if !ok {
		return 0, errBadDstAddress
	}
	var path *Path
	if c.local.IA != sdst.IA {
		path = c.selector.ReplyPath(c.local, sdst)
		if path == nil {
			return 0, errNoPathTo(sdst.IA)
		}
	}
	return c.WriteToPath(b, sdst, path)
}

func (c *listener) WriteToPath(b []byte, dst UDPAddr, path *Path) (int, error) {
	return c.baseUDPConn.writeMsg(c.local, dst, path, b)
}

func (c *listener) Close() error {
	stats.unsubscribe(c.selector)
	// FIXME: multierror!
	_ = c.selector.Close()
	return c.baseUDPConn.Close()
}

type DefaultReplySelector struct {
	mtx     sync.RWMutex
	remotes map[udpAddrKey]remoteEntry
}

func NewDefaultReplySelector() *DefaultReplySelector {
	return &DefaultReplySelector{
		remotes: make(map[udpAddrKey]remoteEntry),
	}
}

func (s *DefaultReplySelector) ReplyPath(src, dst UDPAddr) *Path {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	r, ok := s.remotes[makeKey(dst)]
	if !ok || len(r.paths) == 0 {
		return nil
	}
	return r.paths[0]
}

func (s *DefaultReplySelector) OnPacketReceived(src, dst UDPAddr, path *Path) {
	if path == nil {
		return
	}

	s.mtx.Lock()
	defer s.mtx.Unlock()

	ksrc := makeKey(src)
	r := s.remotes[ksrc]
	r.seen = time.Now()
	r.paths.insert(path, defaultSelectorMaxReplyPaths)
	s.remotes[ksrc] = r
}

func (s *DefaultReplySelector) OnPathDown(PathFingerprint, PathInterface) {

}

func (s *DefaultReplySelector) Close() error {
	return nil
}

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

type remoteEntry struct {
	paths pathsMRU
	seen  time.Time
}

// pathsMRU is a list tracking the most recently used (inserted) path
type pathsMRU []*Path

func (p *pathsMRU) insert(path *Path, maxEntries int) {
	paths := *p
	i := 0
	for ; i < len(paths); i++ {
		if paths[i].Fingerprint == path.Fingerprint {
			break
		}
	}
	if i == len(paths) {
		if len(paths) < maxEntries {
			*p = append(paths, nil)
			paths = *p
		} else {
			i = len(paths) - 1 // overwrite least recently used
		}
	}
	paths[i] = path

	// move most-recently-used to front
	if i != 0 {
		pi := paths[i]
		copy(paths[1:i+1], paths[0:i])
		paths[0] = pi
	}
}
