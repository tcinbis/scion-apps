package ios

import (
	"fmt"

	"github.com/netsec-ethz/scion-apps/pkg/pan"
)

type SelectorObserver interface {
	PathsWillChange()
	PathsDidChange()

	PathDidGoDown(path *Path)
}

// DefaultSelector is beefed up version of pan.DefaultSelector
type defaultSelector struct {
	// mutex              	sync.Mutex
	paths              	[]*pan.Path
	observer 			SelectorObserver // nonnull!
}

func (s *defaultSelector) AllPaths() []*pan.Path {
	if len(s.paths) == 0 {
		return nil
	}
	return s.paths
}

func (s *defaultSelector) Path() *pan.Path {
	if len(s.paths) == 0 {
		return nil
	}
	return s.paths[0]
}

func (s *defaultSelector) SetPaths(paths []*pan.Path) {
	if s.observer != nil {
		s.observer.PathsWillChange()
	}
	s.paths = paths
	if s.observer != nil {
		s.observer.PathsDidChange()
	}
}

func (s *defaultSelector) OnPathDown(pf pan.PathFingerprint, pi pan.PathInterface) {
	for _, path := range s.paths {
		if pan.IsInterfaceOnPath(path, pi) || pf == path.Fingerprint {
			fmt.Println("down:", path, len(s.paths))
			s.observer.PathDidGoDown(&Path { underlying: path })
		}
	}
}
