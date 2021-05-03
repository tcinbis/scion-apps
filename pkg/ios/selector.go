package ios

import (
	"fmt"
	"sync"

	"github.com/netsec-ethz/scion-apps/pkg/pan"
)

type SelectorObserver interface {
	PathsWillChange()
	PathsDidChange()
	
	CurrentPathWillChange()
	CurrentPathDidChange()

	CurrentPathDidGoDown()
}

// DefaultSelector is beefed up version of pan.DefaultSelector
type defaultSelector struct {
	mutex              	sync.Mutex
	paths              	[]*pan.Path
	current            	*pan.Path
	pathFixed 		 	bool
	observer 			SelectorObserver
}

func (s *defaultSelector) IsPathFixed() bool {
	return s.pathFixed
}

func (s *defaultSelector) FixPath(path *pan.Path, check bool) bool {
	if s.observer != nil { s.observer.CurrentPathWillChange() }
	s.mutex.Lock()
	defer func() {
		s.mutex.Unlock()
		if s.observer != nil { 
			s.observer.CurrentPathDidChange()
		}
	}()

	if path == nil {
		s.pathFixed = false
		if len(s.paths) == 0 {
			s.setCurrent(nil)
		} else {
			s.setCurrent(s.paths[0])
		}
		return true
	}

	found := !check

	if check {
		for _, p := range s.paths {
			if p.Fingerprint == path.Fingerprint {
				found = true
				break
			}
		}
	}

	if found {
		s.pathFixed = true
		s.setCurrent(path)
	}

	return found
}

func (s *defaultSelector) AllPaths() []*pan.Path {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if len(s.paths) == 0 {
		return nil
	}
	return s.paths
}

func (s *defaultSelector) Path() *pan.Path {
	return s.current
}

func (s *defaultSelector) setCurrent(path *pan.Path) {
	s.current = path
}

func (s *defaultSelector) SetPaths(paths []*pan.Path) {
	if s.observer != nil {
		s.observer.CurrentPathWillChange()
		s.observer.PathsWillChange()
	}
	s.mutex.Lock()
	defer func() {
		s.mutex.Unlock()
		if s.observer != nil { 
			s.observer.PathsDidChange()
			s.observer.CurrentPathDidChange()
		}
	}()

	s.paths = paths
	foundCurrentPath := false
	if s.current != nil {
		for _, p := range s.paths {
			if p.Fingerprint == s.current.Fingerprint {
				foundCurrentPath = true
				break
			}
		}
	}

	if !foundCurrentPath {
		if len(s.paths) == 0 {
			s.setCurrent(nil)
		} else {
			s.setCurrent(s.paths[0])
		}
	}

	if s.pathFixed && !foundCurrentPath {
		fmt.Println("Path selector path was fixed but fixed path no longer exists after paths were updated. Exiting fixed path mode.")
		s.pathFixed = false
	}
}

func (s *defaultSelector) OnPathDown(pf pan.PathFingerprint, pi pan.PathInterface) {
	currentPathDown := false
	s.mutex.Lock()
	defer func() {
		s.mutex.Unlock();
		if currentPathDown && s.observer != nil {
			s.observer.CurrentPathDidGoDown()
		}
	}()

	if s.pathFixed { return }

	if pan.IsInterfaceOnPath(s.current, pi) || pf == s.current.Fingerprint {
		currentPathDown = true
		fmt.Println("down:", s.current, len(s.paths))
		better := pan.FirstMoreAlive(s.current, s.paths)
		if better >= 0 {
			// Try next path. Note that this will keep cycling if we get down notifications
			s.setCurrent(s.paths[better])
			fmt.Println("failover:", s.current, len(s.paths))
		}
	}
}
