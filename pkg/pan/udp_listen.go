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

var errBadDstAddress = errors.New("dst address not a UDPAddr")

type Listener interface {
	net.PacketConn

	ReadFromPath(b []byte) (int, UDPAddr, *Path, error)
	//MakeConnectionToRemote(ctx context.Context, remote UDPAddr, policy Policy, selector Selector) (Conn, error)
	GetSelector() ReplySelector
	SetSelector(s ReplySelector)
}

type UDPListener struct {
	baseUDPConn

	local    UDPAddr
	selector ReplySelector
}

func ListenUDP(ctx context.Context, local *net.UDPAddr,
	selector ReplySelector, multi bool) (Listener, error) {

	local, err := defaultLocalAddr(local)
	if err != nil {
		return nil, err
	}

	if selector == nil {
		if multi {
			fmt.Println("Using MultiReplySelector")
			selector = NewMultiReplySelector(context.Background())
		} else {
			selector = NewDefaultReplySelector()
		}
	}
	stats.subscribe(selector)
	raw, slocal, err := openBaseUDPConn(ctx, local)
	if err != nil {
		return nil, err
	}

	return &UDPListener{
		baseUDPConn: baseUDPConn{
			raw: raw,
		},
		local:    slocal,
		selector: selector,
	}, nil
}

//func (c *UDPListener) MakeConnectionToRemote(ctx context.Context, remote UDPAddr, policy Policy, selector Selector) (Conn, error) {
//	if selector == nil {
//		selector = &DefaultSelector{}
//	}
//	fmt.Println("MakeConnectionToRemote called")
//	var subscriber *pathRefreshSubscriber = nil
//	if remote.IA != c.local.IA {
//		// If selector is not already populated with a path give it the reply path that we have
//		if selector.Path() == nil {
//			selector.SetPaths([]*Path{c.selector.ReplyPath(c.local, remote)})
//		}
//
//		subscriber = pathRefreshSubscriberMake(remote, policy, selector)
//		go func() {
//			err := subscriber.attach(ctx)
//			if err != nil {
//				fmt.Printf("Failed to attach path refresh subscriber for UDPListener-connection %v\n", err)
//			}
//		}()
//	}
//
//	return &connection{
//		baseUDPConn: &c.baseUDPConn,
//		isListener:  true,
//		local:       c.local,
//		remote:      remote,
//		subscriber:  subscriber,
//		Selector:    selector,
//	}, nil
//}

func (c *UDPListener) LocalAddr() net.Addr {
	return c.local
}

func (c *UDPListener) ReadFrom(b []byte) (int, net.Addr, error) {
	n, remote, _, err := c.ReadFromPath(b)
	return n, remote, err
}

func (c *UDPListener) ReadFromPath(b []byte) (int, UDPAddr, *Path, error) {
	n, remote, fwPath, err := c.baseUDPConn.readMsg(b)
	if err != nil {
		return n, UDPAddr{}, nil, err
	}
	path, err := reversePathFromForwardingPath(remote.IA, c.local.IA, fwPath)
	c.selector.OnPacketReceived(remote, c.local, path)
	return n, remote, path, err
}

func (c *UDPListener) WriteTo(b []byte, dst net.Addr) (int, error) {
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

func (c *UDPListener) WriteToPath(b []byte, dst UDPAddr, path *Path) (int, error) {
	return c.baseUDPConn.writeMsg(c.local, dst, path, b)
}

func (c *UDPListener) Close() error {
	stats.unsubscribe(c.selector)
	// FIXME: multierror!
	_ = c.selector.Close()
	return c.baseUDPConn.Close()
}

func (c *UDPListener) GetSelector() ReplySelector {
	return c.selector
}

func (c *UDPListener) SetSelector(s ReplySelector) {
	c.selector = s
}
