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
	"sync"
	"time"
)

var stats pathStatsDB

func init() {
	stats = pathStatsDB{
		paths:      make(map[PathFingerprint]PathStats),
		interfaces: make(map[PathInterface]PathInterfaceStats),
	}
}

type PathStats struct {
	Path *Path
	// Was notified down at the recorded time (0 for never notified down)
	IsNotifiedDown time.Time
	// Observed Latency
	Latency []StatsLatencySample
	// Observed CWND
	Cwnd *StatsCwndSample
}

func NewPathStats(p *Path) PathStats {
	return PathStats{
		Path:    p,
		Cwnd:    nil,
		Latency: make([]StatsLatencySample, 0),
	}
}

type PathInterfaceStats struct {
	// Was notified down at the recorded time (0 for never notified down)
	IsNotifiedDown time.Time
}

type StatsLatencySample struct {
	Time  time.Time
	Value time.Duration
}

type StatsCwndSample struct {
	Time  time.Time
	Value uint64
}

type pathDownNotifyee interface {
	OnPathDown(PathFingerprint, PathInterface)
}

type pathStatsDB struct {
	mutex sync.RWMutex
	// TODO: this needs a fixed/max capacity and least-recently-used spill over
	// Possibly use separate, explicitly controlled table for paths in dialed connections.
	paths map[PathFingerprint]PathStats
	// TODO: this should rather be "link" or "hop" stats, i.e. identified by two
	// consecutive (unordered?) interface IDs.
	interfaces map[PathInterface]PathInterfaceStats

	subscribers []pathDownNotifyee
}

func (s *pathStatsDB) RegisterLatency(p *Path, latency time.Duration) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	ps, ok := s.paths[p.Fingerprint]
	if !ok {
		ps = NewPathStats(p)
	}
	if len(ps.Latency) < statsNumLatencySamples {
		ps.Latency = append(ps.Latency, StatsLatencySample{
			Time:  time.Now(),
			Value: latency,
		})
	} else {
		copy(ps.Latency[0:statsNumLatencySamples-1], ps.Latency[1:statsNumLatencySamples])
		ps.Latency[statsNumLatencySamples-1] = StatsLatencySample{
			Time:  time.Now(),
			Value: latency,
		}
	}
	s.paths[p.Fingerprint] = ps
}

func (s *pathStatsDB) RegisterPath(p *Path) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if _, ok := s.paths[p.Fingerprint]; !ok {
		s.paths[p.Fingerprint] = NewPathStats(p)
	}
}

func (s *pathStatsDB) RegisterCwnd(p *Path, cwnd uint64) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	ps, ok := s.paths[p.Fingerprint]
	if !ok {
		ps = NewPathStats(p)
	}
	ps.Cwnd = &StatsCwndSample{
		Time:  time.Now(),
		Value: cwnd,
	}
	ps.Path.Metadata.Cwnd = cwnd
	s.paths[p.Fingerprint] = ps
}

func (s *pathStatsDB) GetPathCwnd(p PathFingerprint) uint64 {
	if ps, ok := s.paths[p]; ok {
		if ps.Cwnd != nil {
			return ps.Cwnd.Value
		}
	}
	return 0
}

func PathsWithoutCwnd() []*Path {
	return stats.PathsWithoutCwnd()
}

func (s *pathStatsDB) PathsWithoutCwnd() []*Path {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	missing := make([]*Path, 0)

	for _, ps := range s.paths {
		if ps.Cwnd == nil {
			continue
		}
		missing = append(missing, ps.Path)
	}

	return missing
}

func FirstMoreAlive(p *Path, paths []*Path) int {
	return stats.FirstMoreAlive(p, paths)
}

// FirstMoreAlive returns the index of the first path in paths that is strictly "more
// alive" than p, or -1 if there is none.
// A path is considered to be more alive if it does not contain any of p's interfaces that
// are considered down
func (s *pathStatsDB) FirstMoreAlive(p *Path, paths []*Path) int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	for i, pc := range paths {
		if s.IsMoreAlive(pc, p) {
			return i
		}
	}
	return -1
}

// IsMoreAlive checks if a is strictly "less down" / "more alive" than b.
// Returns true if a does not have any recent down notifications and b does, or
// (more generally) if all down notifications for a are strictly older
// than any down notificating for b.
func (s *pathStatsDB) IsMoreAlive(a, b *Path) bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	// newest relevant down notification for path A
	newestA := s.paths[a.Fingerprint].IsNotifiedDown
	if a.Metadata != nil {
		for _, pi := range a.Metadata.Interfaces {
			if v, ok := s.interfaces[pi]; ok {
				if v.IsNotifiedDown.After(newestA) {
					newestA = v.IsNotifiedDown
				}
			}
		}
	}

	// oldest relevant down notification for path B
	t0 := time.Time{}
	oldestB := s.paths[b.Fingerprint].IsNotifiedDown
	if b.Metadata != nil {
		for _, pi := range b.Metadata.Interfaces {
			if v, ok := s.interfaces[pi]; ok && v.IsNotifiedDown != t0 {
				if oldestB == t0 || v.IsNotifiedDown.Before(oldestB) {
					oldestB = v.IsNotifiedDown
				}
			}
		}
	}
	return newestA.Before(oldestB.Add(-pathDownNotificationTimeout)) // XXX: what is this value, what does it mean?
}

func (s *pathStatsDB) subscribe(subscriber pathDownNotifyee) {
	s.subscribers = append(s.subscribers, subscriber)
}

func (s *pathStatsDB) unsubscribe(subscriber pathDownNotifyee) {
	idx := -1
	for i, v := range s.subscribers {
		if subscriber == v {
			idx = i
			break
		}
	}
	if idx >= 0 {
		s.subscribers = append(s.subscribers[:idx], s.subscribers[idx+1:]...)
	}
}

func (s *pathStatsDB) NotifyPathDown(pf PathFingerprint, pi PathInterface) {
	s.recordPathDown(pf, pi)
	for _, subscriber := range s.subscribers {
		subscriber.OnPathDown(pf, pi)
	}
}

func (s *pathStatsDB) recordPathDown(pf PathFingerprint, pi PathInterface) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	now := time.Now()
	ps := s.paths[pf]
	ps.IsNotifiedDown = now
	s.paths[pf] = ps

	pis := s.interfaces[pi]
	pis.IsNotifiedDown = now
	s.interfaces[pi] = pis
}
