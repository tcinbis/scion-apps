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

// Policy is a stateless filter / sorter for paths.
type Policy interface {
	// Filter and prioritize paths
	Filter(paths []Path, local, remote UDPAddr) []Path
}

// PolicyChain applies multiple policies in order.
type PolicyChain struct {
	Policies []Policy
}

func (p *PolicyChain) Filter(paths []Path, src, dst UDPAddr) []Path {
	for _, p := range p.Policies {
		paths = p.Filter(paths, src, dst)
	}
	return paths
}

// TODO:
// - filter by beaconing metadata (latency, bandwidth, geo, MTU, ...)
// - order by beaconing metadata.
//   Partial order as information may be incomplete and not comparable -- pick stable order.
// - policy language (yaml or whatever)

// Pinned is a policy that keeps only a preselected set of paths.
// This can be used to implement interactive hard path selection.
type Pinned struct {
	sequence []pathFingerprint
}

func (p *Pinned) Filter(paths []Path, src, dst UDPAddr) []Path {
	filtered := make([]Path, 0, len(p.sequence))
	for _, s := range p.sequence {
		for _, p := range paths {
			if fingerprint(p) == s {
				filtered = append(filtered, p)
				break
			}
		}
	}
	return filtered
}

// Preferred is a policy that keeps all paths but moves a few preselected paths to the top.
// This can be used to implement interactive path preference with failover to other paths.
type Preferred struct {
	sequence []pathFingerprint
}

func (p *Preferred) Filter(paths []Path, src, dst UDPAddr) []Path {
	filtered := make([]Path, 0, len(p.sequence))
	for _, s := range p.sequence {
		for _, p := range paths {
			if fingerprint(p) == s {
				filtered = append(filtered, p)
				break
			}
		}
	}
	return filtered
}
