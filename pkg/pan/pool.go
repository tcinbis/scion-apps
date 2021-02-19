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
	"sync"
	"time"
)

// XXX
// Why the global pool:
// - share cache between multiple connections
// - share stats
// - have something central that does refresh before expiration
// this is all private stuff
// if we need to implement the basic policies with "watchers" or "path handles" with callbacks or
// something then this should be possible without API changes
// also, whether or not we have this pool thing is also private
var pool pathPool

// pathPoolDst is path pool entry for one destination IA
type pathPoolDst struct {
	lastQuery      time.Time
	earliestExpiry time.Time
	paths          []*Path
}

type pathPool struct {
	refresher    refresher
	entriesMutex sync.RWMutex
	entries      map[IA]pathPoolDst
}

func init() {
	// XXX: not very elegant, but I do like to separate out the refresher _and_
	// keep the subscribe interface on the pool. /shrug
	pool.refresher = refresher{
		pool:            &pool,
		subscribers:     make(map[IA][]subscriber),
		newSubscription: make(chan bool),
	}
	pool.entries = make(map[IA]pathPoolDst)
	// XXX: bah. in init? It doesn't do anything until it's used, but still meh.
	go pool.refresher.run()
}

func (p *pathPool) subscribe(ctx context.Context, dstIA IA, s subscriber) ([]*Path, error) {
	return p.refresher.subscribe(ctx, dstIA, s)
}

func (p *pathPool) unsubscribe(dstIA IA, s subscriber) {
	p.refresher.unsubscribe(dstIA, s)
}

// paths returns paths to dstIA. This _may_ query paths, unless they have recently been queried.
func (p *pathPool) paths(ctx context.Context, dstIA IA) ([]*Path, error) {
	p.entriesMutex.RLock()
	if entry, ok := p.entries[dstIA]; ok {
		if time.Since(entry.lastQuery) > pathRefreshMinInterval {
			defer p.entriesMutex.RUnlock()
			return append([]*Path{}, entry.paths...), nil
		}
	}
	p.entriesMutex.RUnlock()
	return p.queryPaths(ctx, dstIA)
}

// queryPaths returns paths to dstIA. Unconditionally requests paths from sciond.
func (p *pathPool) queryPaths(ctx context.Context, dstIA IA) ([]*Path, error) {
	paths, err := host().queryPaths(ctx, dstIA)
	if err != nil {
		return nil, err
	}
	p.entriesMutex.Lock()
	defer p.entriesMutex.Unlock()
	entry := p.entries[dstIA]
	entry.update(paths)
	p.entries[dstIA] = entry
	return append([]*Path{}, paths...), nil
}

// cachedPaths returns paths to dstIA. Always returns the cached paths, never queries paths.
func (p *pathPool) cachedPaths(dst IA) []*Path {
	p.entriesMutex.RLock()
	defer p.entriesMutex.RUnlock()
	return append([]*Path{}, p.entries[dst].paths...)
}

func (p *pathPool) entry(dstIA IA) (pathPoolDst, bool) {
	p.entriesMutex.RLock()
	defer p.entriesMutex.RUnlock()
	e, ok := p.entries[dstIA]
	return e, ok
}

func (e *pathPoolDst) update(paths []*Path) {
	now := time.Now()
	expiryDropTime := now.Add(-pathExpiryPruneLeadTime)

	// the updated entry includes all new paths.
	// Any non-expired old path not included in the new paths is appended to the
	// back (but in same order)
	newPathSet := make(map[pathFingerprint]struct{}, len(paths))
	for _, p := range paths {
		newPathSet[p.Fingerprint] = struct{}{}
	}
	for _, old := range e.paths {
		if _, ok := newPathSet[old.Fingerprint]; !ok && old.Expiry.After(expiryDropTime) {
			paths = append(paths, old)
		}
	}

	e.lastQuery = now
	e.earliestExpiry = earliestPathExpiry(paths)
	e.paths = paths
}

func earliestPathExpiry(paths []*Path) time.Time {
	ret := maxTime
	for _, p := range paths {
		expiry := p.Expiry
		if expiry.Before(ret) {
			ret = expiry
		}
	}
	return ret
}
