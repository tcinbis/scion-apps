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
	"bytes"
	"context"
	"encoding/binary"
	"net"
)

/*
Introduction
------------

Path UDP, PUDP, (pronnounced pooh-the-pee, for shits and giggles), is an
EXPERIMENTAL datagram protocol with an explicit path control mechanisms.
The main/basic features are
	* path racing (find the lowest latency path and use it)
	* active path probing to optimise latency and detect faults quickly

The main use case for this is to build a single-path-but-path-optimising QUIC
on top of this, but it can also be used adopted for raw datagram usecases.

PUDP is defined on top of UDP. Each PUDP message consists of a number of command
"headers" and the (optional) payload.

Note that all messages, even a payload-only message, is includes a PUDP header,
making this protocol INCOMPATIBLE with applications speaking plain UDP.
Both sides have to agree upfront to use PUDP, not UDP.
Alternatively, this could be defined as UDP + PUDP commands in SCION end-to-end
extension headers. It's not obvious to me what is "more correct" in terms of
the layer cake. Including this as part of the UDP payload is certainly easier
to implement right now.



Message format
--------------

PUDP message: <command>* <payload>?
  - command: 1-byte op code + stuff
    Unless otherwise noted, continue processing the message's next
    command/payload after each command.
     - payload:   0x00, data until end of message
     - race:      0x01, <sequence number>.
                  Receiver ignores duplicate packets for same sequence number.
                  Receiver keeps N last used sequence numbers.
                  If sequence number is in the list, *drop* this duplicate packet.
                  If sequence number is not in the list:
										Insert sequence number to list, replacing the lowest entry,
										if this lowest entry is smaller than the new sequence
										number.
										If sequence number is larger than all previously seen, the
										receiver should use this path as the return path.

     - ping:      0x10, <sequence number>.
                  Reply immediately with pong, <sequence number>.
     - pong:      0x11, <sequence number>.
                  Record latency (if this is an expected response)

     - identify:  0x20
		 - me:				0x21, <interface list>
									Reply to a `identify`. To indicate that this is an instance
									of an anycasted service where different AS interfaces may
									lead to different instances. List of interfaces that will
									reach this instance.
									Interface list is empty if all interfaces may lead to this
									instance.

     - prefer:		0x22, <path desc>
									Tell the other side that the matching paths are splendid.
									TODO

     - avoid:			0x23, <path desc>
									Tell the other side that the matching paths are meh.
									TODO

  - interface list: 1-byte length N, N times 2-byte interface IDs

  - path desc: TODO



Operation
---------

The client/dialer controls the path, the server/listener uses the path used by
the client's last payload message, unless this path is broken.
This assumes that the path is not voluntarily changed too frequently (not more
often than once every few RTTs)


## Racing

The client starts the connection typically by *racing* the first few messages
over multiple paths; the same payload is sent over multiple paths with a header
identifyng this as a race packet that should be deduplicated.
The server passes the payload to the application and ignores duplicates.
Once it starts responding, it uses the path on which the packet(s) arrived first.
responding on the path on which the first packet arrived.


## Probing

The client probes paths by sending a `ping`. The server replies with a `pong`
immediately/very soon.  On the current path, a ping/pong can piggypack on data
packets.
The rate for probes packets is on the order of once per second per path. When no
payload messages are sent, no probes / replies are sent either.


## Active / backup paths

The client choses a small subset of the available and allowed paths to be
*active paths*, based on the path policy. The remaining paths are considered
backup paths.

The client will apply racing and probing only on the active paths.

If a fault is detected on an active path, the path is relegated replaced with a
path from the backup set.


Unclear
-------

## Leader/follower negotiation

TODO
For bandwidth exploration & optimisation, the sending side needs to be in
control of the path.  Assuming that the "receiver" still sends ACKs, and that
we want to use symmtric paths, an explicit leader/follower mechanism would be
useful. I imagine it could work like this:

- The leader defines the current path with every data packet (0x00 payload "command").
  This path should be used by the follower until a different path is
  "announced", except in case of explicit error notification.
- Initially, the initiator of the connection is the leader
- Both sides maintain a weight of how much they want to lead.
- The command "follow, weight"


## Variable MTU

Max payload size varies; different path headers, different PUDP headers.
How does QUIC cope with this?

Mechanism to allow *querying* max payload size (and other path-related info):
`conn.MaxPayloadSize()` method, freezes the path and PUDP headers, until after
the next Write. Effectively, this evaluates the controller exactly like a Write
would do and caches this (somehow) until the Write.

NOTE: this also applies to the "normal" UDP conn.

*/

// DialPUDP creates a connected PUDP conn.
func DialPUDP(ctx context.Context, local *net.UDPAddr, remote UDPAddr) (net.Conn, error) {

	controller := &pudpController{}

	udpConn, err := DialUDP(ctx, local, remote, controller)
	if err != nil {
		return nil, err
	}

	return &connectedPUDPConn{
		connectedConn: udpConn.(*connectedConn),
		controller:    controller,
	}, nil
}

type connectedPUDPConn struct {
	*connectedConn
	controller *pudpController
}

type pudpHeader struct {
	buf *bytes.Buffer
}

func (h *pudpHeader) race(seq uint16) {
	h.buf.WriteByte(0x01)
	binary.Write(h.buf, binary.LittleEndian, seq)
}

func (h *pudpHeader) ping(seq uint16) {
	h.buf.WriteByte(0x10)
	binary.Write(h.buf, binary.LittleEndian, seq)
}

func (h *pudpHeader) pong(seq uint16) {
	h.buf.WriteByte(0x11)
	binary.Write(h.buf, binary.LittleEndian, seq)
}

func (h *pudpHeader) identify() {
	h.buf.WriteByte(0x20)
}

func (h *pudpHeader) Bytes() []byte {
	return h.buf.Bytes()
}

func (c *connectedPUDPConn) Write(b []byte) (int, error) {
	panic("not implemented") // TODO: Implement

	/*
		// Note: the server side does not do any of this.

		header := pudpHeader{} // TODO use some buffer somewhere
		if !c.controller.hasReceivedIdentifier {
			header.identify()
		}
		if c.controller.pingCurrent() {
			header.ping(c.controller.pingSequenceNum())
		}
		if c.controller.isRacing() {
			header.race(c.controller.raceSequenceNum())
			header.payload()
			for _, p := range c.controller.paths() {
				c.connectedConn.writeMsg(header.Serialize() + b, p)
			}
		} else {
			c.connectedConn.Write(header.Bytes() + b)  // XXX: bad copy c/should be avoided
		}
	*/
}

func (c *connectedPUDPConn) Read(b []byte) (int, error) {
	panic("not implemented") // TODO: Implement
}

type pudpController struct {
	remoteIdentifier []IfID
}

func (p *pudpController) Path() (*Path, error) {
	panic("not implemented") // TODO: Implement
}

func (p *pudpController) SetPaths(_ []*Path) {
	panic("not implemented") // TODO: Implement
}

func (p *pudpController) OnPathDown(_ *Path, _ PathInterface) {
	panic("not implemented") // TODO: Implement
}
