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
	Filter(paths []*Path) []*Path
}

type PolicyFunc func(paths []*Path) []*Path

func (f PolicyFunc) Filter(paths []*Path) []*Path {
	return f(paths)
}

// PolicyChain applies multiple policies in order.
type PolicyChain []Policy

func (p PolicyChain) Filter(paths []*Path) []*Path {
	for _, p := range p {
		paths = p.Filter(paths)
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

func (p Pinned) Filter(paths []*Path) []*Path {
	filtered := make([]*Path, 0, len(p.sequence))
	for _, s := range p.sequence {
		for _, path := range paths {
			if path.Fingerprint == s {
				filtered = append(filtered, path)
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

func (p Preferred) Filter(paths []*Path) []*Path {
	filtered := make([]*Path, 0, len(paths))
	for _, s := range p.sequence {
		for _, path := range paths {
			if path.Fingerprint == s {
				filtered = append(filtered, path)
				break
			}
		}
	}
	for _, path := range paths {
		add := true
		for _, s := range p.sequence {
			if path.Fingerprint == s {
				add = false
				break
			}
		}
		if add {
			filtered = append(filtered, path)
		}
	}
	return filtered
}
