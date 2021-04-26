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
)

func DialUDP(ctx context.Context, local *net.UDPAddr, remote UDPAddr,
	policy Policy, selector Selector) (Conn, error) {

	local, err := defaultLocalAddr(local)
	if err != nil {
		return nil, err
	}

	if selector == nil {
		selector = &DefaultSelector{}
	}

	raw, slocal, err := openBaseUDPConn(ctx, local)
	if err != nil {
		return nil, err
	}
	var subscriber *pathRefreshSubscriber
	if remote.IA != slocal.IA {
		subscriber, err = openPathRefreshSubscriber(ctx, remote, policy, selector)
		if err != nil {
			return nil, err
		}
	}
	return &connection{
		baseUDPConn: &baseUDPConn{
			raw: raw,
		},
		isListener: false,
		local:      slocal,
		remote:     remote,
		subscriber: subscriber,
		Selector:   selector,
	}, nil
}
