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

type PathStats struct {
	// Was notified down at the recorded time (0 for never notified down)
	IsNotifiedDown time.Time
	// Observed Latency
	Latency []StatsLatencySample
}

func (ps PathStats) IsDown() bool {
	return time.Since(ps.IsNotifiedDown) < pathDownNotificationTimeout
}

type StatsLatencySample struct {
	Time  time.Time
	Value time.Duration
}

type pathStatsDB struct {
	mutex sync.RWMutex
	stats map[pathFingerprint]PathStats
}

func (s *pathStatsDB) get(p pathFingerprint) PathStats {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.stats[p]
}

func (s *pathStatsDB) observeLatency(p pathFingerprint, latency time.Duration) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	ps := s.stats[p]
	if len(ps.Latency) < statsNumLatencySamples {
		ps.Latency = append(ps.Latency, StatsLatencySample{
			Time:  time.Now(),
			Value: latency,
		})
	} else {
		copy(ps.Latency[0:statsNumLatencySamples-1], ps.Latency[1:statsNumLatencySamples])
		ps.Latency[statsNumLatencySamples] = StatsLatencySample{
			Time:  time.Now(),
			Value: latency,
		}
	}
	s.stats[p] = ps
}

func (s *pathStatsDB) notifyDown(p pathFingerprint) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	ps := s.stats[p]
	ps.IsNotifiedDown = time.Now()
	s.stats[p] = ps
}

var stats pathStatsDB

func init() {
	stats = pathStatsDB{
		stats: make(map[pathFingerprint]PathStats),
	}
}

func Stats(path pathFingerprint) PathStats {
	return stats.get(path)
}
