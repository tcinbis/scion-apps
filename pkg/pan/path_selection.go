package pan

import (
	"context"
	"fmt"
	"sync"
)

//////////////////// subscriber

type pathRefreshSubscriber struct {
	remote UDPAddr
	policy Policy
	target Selector
}

func pathRefreshSubscriberMake(remote UDPAddr, policy Policy,
	target Selector) *pathRefreshSubscriber {
	s := &pathRefreshSubscriber{
		target: target,
		policy: policy,
		remote: remote,
	}
	
	return s
}

func (s *pathRefreshSubscriber) attach(ctx context.Context) error {
	paths, err := pool.subscribe(ctx, s.remote.IA, s)
	if err != nil {
		return err
	}
	s.setFiltered(paths)
	return nil
}

func openPathRefreshSubscriber(ctx context.Context, remote UDPAddr, policy Policy,
	target Selector) (*pathRefreshSubscriber, error) {

	s := pathRefreshSubscriberMake(remote, policy, target)

	err := s.attach(ctx)
	if (err != nil) { return nil, err }

	return s, nil
}

func (s *pathRefreshSubscriber) Close() error {
	pool.unsubscribe(s.remote.IA, s)
	return nil
}

func (s *pathRefreshSubscriber) setPolicy(policy Policy) {
	s.policy = policy
	s.setFiltered(pool.cachedPaths(s.remote.IA))
}

func (s *pathRefreshSubscriber) refresh(dst IA, paths []*Path) {
	s.setFiltered(paths)
}

func (s *pathRefreshSubscriber) OnPathDown(pf PathFingerprint, pi PathInterface) {
	s.target.OnPathDown(pf, pi)
}

func (s *pathRefreshSubscriber) setFiltered(paths []*Path) {
	if s.policy != nil {
		paths = s.policy.Filter(paths)
	}
	s.target.SetPaths(paths)
}

//////////////////// selector

// Selector controls the path used by a single **dialed** socket. Stateful.
// The Path() function is invoked for every single packet.
type Selector interface {
	Path() *Path
	SetPaths([]*Path)
	OnPathDown(PathFingerprint, PathInterface)
}

// DefaultSelector is a Selector for a single dialed socket.
// This will keep using the current path, starting with the first path chosen
// by the policy, as long possible.
// Faults are detected passively via SCMP down notifications; whenever such
// a down notification affects the current path, the DefaultSelector will
// switch to the first path (in the order defined by the policy) that is not
// affected by down notifications.
type DefaultSelector struct {
	mutex              sync.Mutex
	paths              []*Path
	current            int
	currentFingerprint PathFingerprint
	pathFixed 		 	bool
}

func (s *DefaultSelector) FixPath(path *Path) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if path == nil {
		s.pathFixed = false
		s.current = 0
		return true
	}

	for i, p := range s.paths {
		if p.Fingerprint == path.Fingerprint {
				s.current = i
				s.pathFixed = true
				return true
		}
	}

	return false
}

func (s *DefaultSelector) AllPaths() []*Path {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if len(s.paths) == 0 {
		return nil
	}
	return s.paths
}

func (s *DefaultSelector) Path() *Path {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if len(s.paths) == 0 {
		return nil
	}
	return s.paths[s.current]
}

func (s *DefaultSelector) SetPaths(paths []*Path) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.paths = paths
	curr := 0
	foundCurrentPath := false
	if s.currentFingerprint != "" {
		for i, p := range s.paths {
			if p.Fingerprint == s.currentFingerprint {
				curr = i
				foundCurrentPath = true
				break
			}
		}
	}

	if s.pathFixed && !foundCurrentPath {
		fmt.Println("Path selector path was fixed but fixed path no longer exists after paths were updated. Exiting fixed path mode.")
		s.pathFixed = false
	}

	s.current = curr
	if len(s.paths) > 0 {
		s.currentFingerprint = s.paths[s.current].Fingerprint
	}
}

func (s *DefaultSelector) OnPathDown(pf PathFingerprint, pi PathInterface) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.pathFixed { return }

	if isInterfaceOnPath(s.paths[s.current], pi) || pf == s.currentFingerprint {
		fmt.Println("down:", s.current, len(s.paths))
		better := stats.FirstMoreAlive(s.paths[s.current], s.paths)
		if better >= 0 {
			// Try next path. Note that this will keep cycling if we get down notifications
			s.current = better
			fmt.Println("failover:", s.current, len(s.paths))
			s.currentFingerprint = s.paths[s.current].Fingerprint
		}
	}
}
