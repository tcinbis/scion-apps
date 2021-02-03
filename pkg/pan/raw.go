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

	"github.com/scionproto/scion/go/lib/addr"
	"github.com/scionproto/scion/go/lib/snet"
)

// openRawSocket opens new SCION socket.
// Use SCIONPacketConn for now. I think I don't like that it requires the explicit SCMPHandler though.
func openRawSCIONSocket(ctx context.Context, local *net.UDPAddr, scmpHandler snet.SCMPHandler) (*snet.SCIONPacketConn, UDPAddr, error) {
	dispatcher := host().dispatcher
	ia := host().ia

	rconn, port, err := dispatcher.Register(ctx, ia, local, addr.SvcNone)
	if err != nil {
		return nil, UDPAddr{}, err
	}

	conn := snet.NewSCIONPacketConn(rconn, scmpHandler, true)
	slocal := UDPAddr{
		IA:   ia,
		IP:   local.IP,
		Port: int(port),
	}
	return conn, slocal, nil
}
