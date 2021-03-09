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
	Source         IA
	Destination    IA
	ForwardingPath ForwardingPath
	Metadata       *PathMetadata // optional
	Fingerprint    PathFingerprint
	Expiry         time.Time
}

func (p *Path) Copy() *Path {
	if p == nil {
		return nil
	}
	return &Path{
		Source:         p.Source,
		Destination:    p.Destination,
		ForwardingPath: p.ForwardingPath.Copy(),
		Metadata:       p.Metadata.Copy(),
		Fingerprint:    p.Fingerprint,
		Expiry:         p.Expiry,
	}
}

func (p *Path) String() string {
	// TODO: if p.Metadata != nil { return p.Interfaces.String() }
	return string(p.Fingerprint)
}

type GeoCoordinates = snet.GeoCoordinates
type LinkType = snet.LinkType

// XXX: remove
func pathDestination(p *Path) IA {
	return p.Destination
}

// ForwardingPath represents a data plane forwarding path.
type ForwardingPath struct {
	spath    spath.Path
	underlay *net.UDPAddr
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

// reversePathFromForwardingPath creates a Path for the return direction from the information
// on a received packet.
// The created Path includes fingerprint and expiry information.
func reversePathFromForwardingPath(src, dst IA, fwPath ForwardingPath) (*Path, error) {
	if fwPath.IsEmpty() {
		return nil, nil
	}
	// XXX: inefficient, decoding twice! Change this to decode and then both
	// reverse and extract fw info
	if err := fwPath.spath.Reverse(); err != nil {
		return nil, err
	}
	fpi, err := fwPath.forwardingPathInfo()
	if err != nil {
		return nil, err
	}
	fingerprint := pathSequence{
		Source:       dst,
		Destination:  src,
		InterfaceIDs: fpi.interfaceIDs,
	}.Fingerprint()
	return &Path{
		Source:         dst,
		Destination:    src,
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
// This can be used to identify a path by its hop sequence, regardless of which
// path segments it is created from.
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
func (s pathSequence) Fingerprint() PathFingerprint {
	// XXX: currently somewhat human readable, could do simple binary
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s", s.Source, s.Destination)
	for _, ifID := range s.InterfaceIDs {
		fmt.Fprintf(&b, " %d", ifID)
	}
	return PathFingerprint(b.String())
}

// XXX: rename. "PathSequenceKey"?
// PathFingerprint is an opaque identifier for a path. It identifies a path by
// its source and destination IA and the sequence of interface identifiers
// along the path.
type PathFingerprint string

func pathFingerprints(paths []*Path) []PathFingerprint {
	fingerprints := make([]PathFingerprint, len(paths))
	for i, p := range paths {
		fingerprints[i] = p.Fingerprint
	}
	return fingerprints
}
