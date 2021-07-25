package pan

import (
	"bufio"
	"context"
	"fmt"
	"github.com/bclicn/color"
	"github.com/scionproto/scion/go/lib/addr"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TODO: Increase timeout to something more realistic
const remoteTimeout = 5 * time.Second

// ReplySelector selects the reply path for WriteTo in a listener.
type ReplySelector interface {
	ReplyPath(src, dst UDPAddr) *Path
	OnPacketReceived(src, dst UDPAddr, path *Path)
	OnPathDown(PathFingerprint, PathInterface)
	SetFixedPath(dst UDPAddr, path *Path)
	AvailablePaths()
	Close() error
}

// udpAddrKey converts a destination's address in a key for maps
type udpAddrKey struct {
	IA   addr.IA
	IP   [16]byte
	Port int
}

// remoteEntry stores paths to destination. Used in ReplySelector
type remoteEntry struct {
	paths       pathsMRU
	seen        time.Time
	expireTimer *time.Timer
	expired     func()
}

// pathsMRU is a list tracking the most recently used (inserted) path
type pathsMRU []*Path

type DefaultReplySelector struct {
	mtx       sync.RWMutex
	remotes   map[udpAddrKey]remoteEntry
	fixedPath *Path
}

// MultiReplySelector is capable of handling multiple destinations while subscribing to Pool updates
type MultiReplySelector struct {
	DefaultReplySelector

	ctx    context.Context
	cancel context.CancelFunc
	ticker *time.Ticker

	iaRemotes map[addr.IA][]udpAddrKey
	iaPaths   map[addr.IA][]*Path

	fixedPath map[udpAddrKey]*Path
}

var (
	_ ReplySelector = &DefaultReplySelector{}
	_ ReplySelector = &MultiReplySelector{}
)

func NewDefaultReplySelector() *DefaultReplySelector {
	return &DefaultReplySelector{
		remotes: make(map[udpAddrKey]remoteEntry),
	}
}

func NewMultiReplySelector(ctx context.Context) *MultiReplySelector {

	rCtx, rCancel := context.WithCancel(ctx)
	selector := &MultiReplySelector{
		DefaultReplySelector: DefaultReplySelector{
			remotes: make(map[udpAddrKey]remoteEntry),
		},
		ctx:       rCtx,
		cancel:    rCancel,
		ticker:    time.NewTicker(10 * time.Second),
		iaRemotes: make(map[addr.IA][]udpAddrKey),
		iaPaths:   make(map[addr.IA][]*Path),
		fixedPath: make(map[udpAddrKey]*Path),
	}

	go selector.run()

	return selector
}

func (s *DefaultReplySelector) ReplyPath(src, dst UDPAddr) *Path {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	if s.fixedPath != nil {
		return s.fixedPath
	}

	r, ok := s.remotes[makeKey(dst)]
	if !ok || len(r.paths) == 0 {
		return nil
	}
	return r.paths[0]
}

func (s *DefaultReplySelector) OnPacketReceived(src, dst UDPAddr, path *Path) {
	if path == nil {
		return
	}

	s.mtx.Lock()
	defer s.mtx.Unlock()

	ksrc := makeKey(src)
	r := s.remotes[ksrc]
	r.seen = time.Now()
	r.paths.insert(path, defaultSelectorMaxReplyPaths)
	s.remotes[ksrc] = r
}

func (s *DefaultReplySelector) OnPathDown(PathFingerprint, PathInterface) {
	fmt.Println("PathDown event missed/ignored in DefaultReplySelector")
}

func (s *DefaultReplySelector) Close() error {
	return nil
}

func (s *DefaultReplySelector) AvailablePaths() {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	for key, val := range s.remotes {
		fmt.Printf("%s, %v, %s \n", key.String(), val.paths.string(), val.seen.String())
	}
}

func (s *DefaultReplySelector) SetFixedPath(dst UDPAddr, path *Path) {
	s.mtx.RLock()
	currFixed := s.fixedPath
	s.mtx.RUnlock()

	if currFixed != path {
		s.mtx.Lock()
		defer s.mtx.Unlock()
		s.fixedPath = path
	}
}

func (s *MultiReplySelector) SetFixedPath(dst UDPAddr, path *Path) {
	ukey := makeKey(dst)
	s.mtx.RLock()
	currFixed := s.fixedPath[ukey]
	s.mtx.RUnlock()

	if currFixed != path {
		s.mtx.Lock()
		defer s.mtx.Unlock()
		s.fixedPath[ukey] = path
	}
}

func (s *MultiReplySelector) ReplyPath(src, dst UDPAddr) *Path {
	ukey := makeKey(dst)
	s.mtx.RLock()
	p, ok := s.fixedPath[ukey]
	s.mtx.RUnlock()
	if !ok {
		if p = s.DefaultReplySelector.ReplyPath(src, dst); p == nil {
			// only use the iaPaths if we have no fixed path and no reply path
			s.mtx.RLock()
			paths, ok := s.iaPaths[dst.IA]
			s.mtx.RUnlock()
			if ok {
				p = paths[0]
			}
		}
	}
	return p
}

// updateRemotes keeps track of the available paths for a given remote UDPAddr
func (s *MultiReplySelector) updateRemotes(src, dst UDPAddr, path *Path) {
	if path == nil {
		return
	}

	ksrc := makeKey(src)
	s.mtx.Lock()
	r, ok := s.remotes[ksrc]
	s.mtx.Unlock()
	r.seen = time.Now()
	if !ok {
		r.expireTimer = time.NewTimer(remoteTimeout)
	} else {
		if !r.expireTimer.Stop() {
			<-r.expireTimer.C
		}
		r.expireTimer.Reset(remoteTimeout)
	}

	r.expired = func() {
		for {
			s.mtx.Lock()
			remote, ok := s.remotes[ksrc]
			s.mtx.Unlock()
			if !ok {
				// remote was already removed!
				return
			}
			select {
			case <-remote.expireTimer.C:
				s.mtx.Lock()
				defer s.mtx.Unlock()
				delete(s.remotes, ksrc)

				// remove this remote from the s.iaRemotes list
				if remotes, ok := s.iaRemotes[ksrc.IA]; ok {
					for i, rem := range remotes {
						if rem == ksrc {
							lr := len(remotes)
							if lr > 1 {
								// replace item to delete with last item in list and cut last item off
								remotes[i] = remotes[lr-1]
								remotes = remotes[:lr-1]
							} else {
								// there is only one item so set empty slice
								remotes = []udpAddrKey{}
							}

							s.iaRemotes[ksrc.IA] = remotes
						}
					}
				}

				fmt.Printf("Deleting %s from remotes after expirey\n", ksrc.String())
				return
			default:
				time.Sleep(5 * time.Second)
			}
		}
	}
	if !ok {
		// only start expired checker with new remote
		go r.expired()
	}
	r.paths.insert(path, defaultSelectorMaxReplyPaths)
	s.remotes[ksrc] = r
}

// updateIA keeps track of the open remotes for an IA contained in the UDPAddr
func (s *MultiReplySelector) updateIA(src, dst UDPAddr, path *Path) {
	if path == nil {
		return
	}
	kSrc := makeKey(src)

	s.mtx.Lock()
	defer s.mtx.Unlock()
	if _, ok := s.iaRemotes[kSrc.IA]; !ok {
		// we got a new IA -> subscribe for updates
		paths, err := pool.subscribe(s.ctx, kSrc.IA, s)
		if err != nil {
			fmt.Printf("Error subsribing to pool updates for %s\n", kSrc.IA.String())
			os.Exit(-1)
		}
		s.iaPaths[kSrc.IA] = paths
	}
	if _, ok := s.remotes[kSrc]; !ok {
		// we got a new remote we have to add to our IA to remotes mapping
		s.iaRemotes[kSrc.IA] = append(s.iaRemotes[kSrc.IA], kSrc)
	}
}

func (s *MultiReplySelector) run() {
	for {
		select {
		case <-s.ctx.Done():
			s.ticker.Stop()
			fmt.Println("MultiReplySelector stopping.")
			break
		case <-s.ticker.C:
			// TODO: perform updates and check for new subscribers
			fmt.Print("Do you want to perform path selection for remotes? [y/N]: ")
			scanner := bufio.NewScanner(os.Stdin)

			scanner.Scan()
			choice := scanner.Text()
			if choice != "yes" && choice != "y" {
				continue
			}
			remote, err := s.chooseRemoteInteractive()
			if err != nil {
				fmt.Printf("Error choosing remote: %v \n", err)
				continue
			}
			path, err := s.choosePathInteractive(remote)
			if err != nil {
				fmt.Printf("Error choosing path: %v \n", err)
				continue
			}
			s.SetFixedPath(remote.ToUDPAddr(), path)

		}
	}
}

func (s *MultiReplySelector) chooseRemoteInteractive() (udpAddrKey, error) {
	fmt.Printf("Available remotes: \n")
	indexToRemote := make(map[int]udpAddrKey)
	i := 0
	for remote, _ := range s.remotes {
		fmt.Printf("[%2d] %s\n", i, remote.String())
		indexToRemote[i] = remote
		i++
	}

	var selectedRemote udpAddrKey
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Printf("Choose remote: ")
	scanner.Scan()
	remoteIndexStr := scanner.Text()
	remoteIndex, err := strconv.Atoi(remoteIndexStr)
	if err == nil && 0 <= remoteIndex && remoteIndex < len(s.remotes) {
		selectedRemote = indexToRemote[remoteIndex]
	} else {
		fmt.Printf("ERROR: Invalid remote index %v, valid indices range: [0, %v]\n", remoteIndex, len(s.remotes)-1)
	}

	re := regexp.MustCompile(`\d{1,4}-([0-9a-f]{1,4}:){2}[0-9a-f]{1,4}`)
	fmt.Printf("Using path:\n %s\n", re.ReplaceAllStringFunc(fmt.Sprintf("%s", selectedRemote), color.Cyan))
	return selectedRemote, nil
}

func (s *MultiReplySelector) choosePathInteractive(remote udpAddrKey) (path *Path, err error) {
	paths := s.iaPaths[remote.IA]

	fmt.Printf("Available paths to %s\n", remote.String())
	for i, path := range paths {
		fmt.Printf("[%2d] %s\n", i, fmt.Sprintf("%s", path))
	}

	var selectedPath *Path
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Printf("Choose path: ")
	scanner.Scan()
	pathIndexStr := scanner.Text()
	pathIndex, err := strconv.Atoi(pathIndexStr)
	if err == nil && 0 <= pathIndex && pathIndex < len(paths) {
		selectedPath = paths[pathIndex]
	} else {
		fmt.Printf("ERROR: Invalid path index %v, valid indices range: [0, %v]\n", pathIndex, len(paths)-1)
	}

	re := regexp.MustCompile(`\d{1,4}-([0-9a-f]{1,4}:){2}[0-9a-f]{1,4}`)
	fmt.Printf("Using path:\n %s\n", re.ReplaceAllStringFunc(fmt.Sprintf("%s", selectedPath), color.Cyan))
	return selectedPath, nil
}

func (s *MultiReplySelector) Close() error {
	s.cancel()
	return nil
}

func (s *MultiReplySelector) refresh(dst addr.IA, paths []*Path) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	fmt.Printf("Received update for %s\n", dst.String())
	s.iaPaths[dst] = paths
}

func (s *MultiReplySelector) OnPacketReceived(src, dst UDPAddr, path *Path) {
	s.updateIA(src, dst, path)
	s.updateRemotes(src, dst, path)
}

func (s *MultiReplySelector) ActiveRemotes() {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	var sb strings.Builder
	if len(s.iaRemotes) == 0 {
		sb.WriteString("No active connections.")
	}

	for _, val := range s.iaRemotes {
		for _, rem := range val {
			path := s.ReplyPath(UDPAddr{}, rem.ToUDPAddr())
			if path.Metadata == nil {
				go path.FetchMetadata()
			}

			sb.WriteString(fmt.Sprintf("%s on path: %s \n", rem.String(), path.String()))
		}
	}
	fmt.Println(sb.String())
}

func (s *MultiReplySelector) AvailablePaths() {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	var sb strings.Builder
	for key, val := range s.iaPaths {
		sb.WriteString(fmt.Sprintf("%s ", key.String()))
		for _, path := range val {
			path.FetchMetadata()
			sb.WriteString(fmt.Sprintf("%s \n", path.String()))
		}
	}
	fmt.Println(sb.String())
}

func (u *udpAddrKey) String() string {
	return fmt.Sprintf("%s,[%s:%d]", u.IA.String(), net.IP(u.IP[:]).String(), u.Port)
}

func (u *udpAddrKey) ToUDPAddr() UDPAddr {
	return UDPAddr{
		IA:   u.IA,
		IP:   u.IP[:],
		Port: u.Port,
	}
}

func makeKey(a UDPAddr) udpAddrKey {
	k := udpAddrKey{
		IA:   a.IA,
		Port: a.Port,
	}
	copy(k.IP[:], a.IP.To16())
	return k
}

func (p *pathsMRU) insert(path *Path, maxEntries int) {
	paths := *p
	i := 0
	for ; i < len(paths); i++ {
		if paths[i].Fingerprint == path.Fingerprint {
			break
		}
	}
	if i == len(paths) {
		if len(paths) < maxEntries {
			*p = append(paths, nil)
			paths = *p
		} else {
			i = len(paths) - 1 // overwrite least recently used
		}
	}

	if path.Metadata == nil && paths[i] != nil {
		// copy over the old metadata
		if path.Fingerprint != paths[i].Fingerprint {
			panic("Would have copied incorrect metadata")
		}
		path.Metadata = paths[i].Metadata
	}
	paths[i] = path

	// move most-recently-used to front
	if i != 0 {
		pi := paths[i]
		copy(paths[1:i+1], paths[0:i])
		paths[0] = pi
	}
}

func (p *pathsMRU) string() string {
	var sb strings.Builder
	for _, path := range *p {
		path.FetchMetadata()
		sb.WriteString(path.String())
	}
	return sb.String()
}
