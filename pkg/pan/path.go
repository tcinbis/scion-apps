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
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/scionproto/scion/go/lib/slayers/path"
	"github.com/scionproto/scion/go/lib/slayers/path/scion"
	"github.com/scionproto/scion/go/lib/snet"
	"github.com/scionproto/scion/go/lib/spath"
)

// XXX: revisit: is this a pointer or value type?
type Path struct {
	// XXX: think about this again. what goes where? should ForwardingPath be exported?
	ForwardingPath ForwardingPath
	Metadata       *PathMetadata // optional
	Fingerprint    pathFingerprint
	Expiry         time.Time
}

func (p *Path) Copy() *Path {
	if p == nil {
		return nil
	}
	return &Path{
		ForwardingPath: p.ForwardingPath.Copy(),
		Metadata:       p.Metadata.Copy(),
		Fingerprint:    p.Fingerprint,
		Expiry:         p.Expiry,
	}
}

func (p *Path) String() string {
	return p.Fingerprint
}

// XXX: copied from snet.PathMetadata: does not contain Expiry and uses the
// local types (pan.PathInterface instead of snet.PathInterface, ...)

// PathMetadata contains supplementary information about a path.
//
// The information about MTU, Latency, Bandwidth etc. are based solely on data
// contained in the AS entries in the path construction beacons. These entries
// are signed/verified based on the control plane PKI. However, the
// *correctness* of this meta data has *not* been checked.
type PathMetadata struct {
	// Interfaces is a list of interfaces on the path.
	Interfaces []PathInterface

	// MTU is the maximum transmission unit for the path, in bytes.
	MTU uint16

	// Latency lists the latencies between any two consecutive interfaces.
	// Entry i describes the latency between interface i and i+1.
	// Consequently, there are N-1 entries for N interfaces.
	// A 0-value indicates that the AS did not announce a latency for this hop.
	Latency []time.Duration

	// Bandwidth lists the bandwidth between any two consecutive interfaces, in Kbit/s.
	// Entry i describes the bandwidth between interfaces i and i+1.
	// A 0-value indicates that the AS did not announce a bandwidth for this hop.
	Bandwidth []uint64

	// Geo lists the geographical position of the border routers along the path.
	// Entry i describes the position of the router for interface i.
	// A 0-value indicates that the AS did not announce a position for this router.
	Geo []GeoCoordinates

	// LinkType contains the announced link type of inter-domain links.
	// Entry i describes the link between interfaces 2*i and 2*i+1.
	LinkType []LinkType

	// InternalHops lists the number of AS internal hops for the ASes on path.
	// Entry i describes the hop between interfaces 2*i+1 and 2*i+2 in the same AS.
	// Consequently, there are no entries for the first and last ASes, as these
	// are not traversed completely by the path.
	InternalHops []uint32

	// Notes contains the notes added by ASes on the path, in the order of occurrence.
	// Entry i is the note of AS i on the path.
	Notes []string
}

func (pm *PathMetadata) Copy() *PathMetadata {
	if pm == nil {
		return nil
	}
	return &PathMetadata{
		Interfaces:   append(pm.Interfaces[:0:0], pm.Interfaces...),
		MTU:          pm.MTU,
		Latency:      append(pm.Latency[:0:0], pm.Latency...),
		Bandwidth:    append(pm.Bandwidth[:0:0], pm.Bandwidth...),
		Geo:          append(pm.Geo[:0:0], pm.Geo...),
		LinkType:     append(pm.LinkType[:0:0], pm.LinkType...),
		InternalHops: append(pm.InternalHops[:0:0], pm.InternalHops...),
		Notes:        append(pm.Notes[:0:0], pm.Notes...),
	}
}

// LowerLatency compares the latency of two paths.
// Returns
//  - true, true if a has strictly lower latency than b
//  - false, true if a has equal or higher latency than b
//  - _, false if not enough information is available to compare a and b
func (a *PathMetadata) LowerLatency(b *PathMetadata) (bool, bool) {
	totA, unknownA := a.latencySum()
	totB, unknownB := b.latencySum()
	if totA < totB && unknownA.subsetOf(unknownB) {
		// total of known smaller and all unknown hops in A are also in B
		return true, true
	} else if totA >= totB && unknownB.subsetOf(unknownA) {
		// total of known larger/equal all unknown hops in B are also in A
		return false, true
	}
	return false, false
}

// latencySum returns the total latency and the set of edges with unknown
// latency
// XXX: the latency from the end hosts to the first/last interface is always
// unknown. If that is taken into account, all the paths become incomparable.
func (pm *PathMetadata) latencySum() (time.Duration, pathHopSet) {
	var sum time.Duration
	unknown := make(pathHopSet)
	for i := 0; i < len(pm.Interfaces)-1; i++ {
		l := pm.Latency[i]
		if l != 0 { // XXX: needs to be fixed in combinator/snet; should not use 0 for unknown
			sum += l
		} else {
			unknown[pathHop{a: pm.Interfaces[i], b: pm.Interfaces[i+1]}] = struct{}{}
		}
	}
	return sum, unknown
}

type PathInterface struct {
	IA   IA
	IfID IfID
}
type GeoCoordinates = snet.GeoCoordinates
type LinkType = snet.LinkType

type pathHop struct {
	a, b PathInterface
}

type pathHopSet map[pathHop]struct{}

func (a pathHopSet) subsetOf(b pathHopSet) bool {
	for x := range a {
		if _, inB := b[x]; !inB {
			return false
		}
	}
	return true
}
func isInterfaceOnPath(p *Path, pi PathInterface) bool {
	for _, c := range p.Metadata.Interfaces {
		if c == pi {
			return true
		}
	}
	return false
}

// pathDestination returns the destination IA of a path.
// XXX: only implemented for paths with metadata.
// XXX: always available, make this a member of Path instead
func pathDestination(p *Path) IA {
	ifaces := p.Metadata.Interfaces
	return ifaces[len(ifaces)-1].IA
}

// ForwardingPath represents a data plane forwarding path.
type ForwardingPath struct {
	spath    spath.Path
	underlay *net.UDPAddr /// XXX not sure it's a good idea to put this here.
}

func (p ForwardingPath) IsEmpty() bool {
	return p.spath.IsEmpty()
}

func (p ForwardingPath) Copy() ForwardingPath {
	return ForwardingPath{
		spath: p.spath.Copy(),
		underlay: &net.UDPAddr{
			IP:   append(net.IP{}, p.underlay.IP...),
			Port: p.underlay.Port,
			Zone: p.underlay.Zone,
		},
	}
}

func (p ForwardingPath) Reversed() (ForwardingPath, error) {
	rev := p.spath.Copy()
	err := rev.Reverse()
	return ForwardingPath{
		spath: rev,
	}, err
}

func (p ForwardingPath) String() string {
	return p.spath.String()
}

func (p ForwardingPath) forwardingPathInfo() (forwardingPathInfo, error) {
	switch p.spath.Type {
	case scion.PathType:
		var sp scion.Decoded
		if err := sp.DecodeFromBytes(p.spath.Raw); err != nil {
			return forwardingPathInfo{}, err
		}
		return forwardingPathInfo{
			expiry:       expiryFromDecoded(sp),
			interfaceIDs: interfaceIDsFromDecoded(sp),
		}, nil
	default:
		return forwardingPathInfo{}, fmt.Errorf("unsupported path type %s", p.spath.Type)
	}
}

// pathFromForwardingPath creates a Path including fingerprint and expiry information from
// the dataplane forwarding path.
func pathFromForwardingPath(src, dst IA, fwPath ForwardingPath) (*Path, error) {
	if fwPath.IsEmpty() {
		return nil, nil
	}
	fpi, err := fwPath.forwardingPathInfo()
	if err != nil {
		return nil, err
	}
	fingerprint := pathSequence{
		Source:       src,
		Destination:  dst,
		InterfaceIDs: fpi.interfaceIDs,
	}.Fingerprint()
	return &Path{
		ForwardingPath: fwPath,
		Expiry:         fpi.expiry,
		Fingerprint:    fingerprint,
	}, nil
}

// forwardingPathInfo contains information extracted from a dataplane forwardng path.
type forwardingPathInfo struct {
	expiry       time.Time
	interfaceIDs []IfID
}

func expiryFromDecoded(sp scion.Decoded) time.Time {
	hopExpiry := func(info *path.InfoField, hf *path.HopField) time.Time {
		ts := time.Unix(int64(info.Timestamp), 0)
		exp := path.ExpTimeToDuration(hf.ExpTime)
		return ts.Add(exp)
	}

	ret := maxTime
	hop := 0
	for i, info := range sp.InfoFields {
		seglen := int(sp.Base.PathMeta.SegLen[i])
		for h := 0; h < seglen; h++ {
			exp := hopExpiry(info, sp.HopFields[hop])
			if exp.Before(ret) {
				ret = exp
			}
			hop++
		}
	}
	return ret
}

func interfaceIDsFromDecoded(sp scion.Decoded) []IfID {
	ifIDs := make([]IfID, 0, 2*len(sp.HopFields)-2*len(sp.InfoFields))

	// return first interface in order of traversal
	first := func(hf *path.HopField, consDir bool) IfID {
		if consDir {
			return IfID(hf.ConsIngress)
		} else {
			return IfID(hf.ConsEgress)
		}
	}
	// return second interface in order of traversal
	second := func(hf *path.HopField, consDir bool) IfID {
		if consDir {
			return IfID(hf.ConsEgress)
		} else {
			return IfID(hf.ConsIngress)
		}
	}

	hop := 0
	for i, info := range sp.InfoFields {
		seglen := int(sp.Base.PathMeta.SegLen[i])
		for h := 0; h < seglen; h++ {
			if h > 0 || (info.Peer && i == 1) {
				ifIDs = append(ifIDs, first(sp.HopFields[hop], info.ConsDir))
			}
			if h < seglen-1 || (info.Peer && i == 0) {
				ifIDs = append(ifIDs, second(sp.HopFields[hop], info.ConsDir))
			}
			hop++
		}
	}

	return ifIDs
}

// pathSequence describes a path by source and dest IA and a sequence of interface IDs.
// This information can be obtained even from the raw forwarding paths.
// This can be used to identify a path regardless of which path segments it is
// created from.
type pathSequence struct {
	Source       IA
	Destination  IA
	InterfaceIDs []IfID
}

func pathSequenceFromInterfaces(interfaces []PathInterface) pathSequence {
	ifIDs := make([]IfID, len(interfaces))
	for i, iface := range interfaces {
		ifIDs[i] = iface.IfID
	}
	return pathSequence{
		Source:       interfaces[0].IA,
		Destination:  interfaces[len(interfaces)-1].IA,
		InterfaceIDs: ifIDs,
	}
}

// Fingerprint returns the pathSequence as a comparable/hashable object (string).
func (s pathSequence) Fingerprint() pathFingerprint {
	// XXX: currently somewhat human readable, could do full binary (or hash like
	// in snet.Fingerprint, but what's the point really?)
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s", s.Source, s.Destination)
	for _, ifID := range s.InterfaceIDs {
		fmt.Fprintf(&b, " %d", ifID)
	}
	return b.String()
}

// XXX: rename. "pathSequenceKey"?
type pathFingerprint = string

func pathFingerprints(paths []*Path) []pathFingerprint {
	fingerprints := make([]pathFingerprint, len(paths))
	for i, p := range paths {
		fingerprints[i] = p.Fingerprint
	}
	return fingerprints
}
