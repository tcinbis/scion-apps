package pan

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/bclicn/color"
	"github.com/scionproto/scion/go/lib/addr"
	"log"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TODO: Increase timeout to something more realistic
const remoteTimeout = 20 * time.Second

// ReplySelector selects the reply path for WriteTo in a listener.
type ReplySelector interface {
	ReplyPath(src, dst UDPAddr) *Path
	OnPacketReceived(src, dst UDPAddr, path *Path)
	OnPathDown(PathFingerprint, PathInterface)
	SetFixedPath(dst UDPAddr, path *Path)
	ClearFixedPath(dst UDPAddr)
	AvailablePaths() string
	RemoteClients() []UdpAddrKey
	Close() error
	Export() ([]byte, error)
}

// UdpAddrKey converts a destination's address in a key for maps
type UdpAddrKey struct {
	IA   addr.IA
	IP   [16]byte
	Port int
}

// RemoteEntry stores paths to destination. Used in ReplySelector
type RemoteEntry struct {
	fixedPath   *Path
	paths       pathsMRU
	seen        time.Time
	expireTimer *time.Timer
	expired     func()
}

// pathsMRU is a list tracking the most recently used (inserted) path
type pathsMRU []*Path

// MultiReplySelector is capable of handling multiple destinations while subscribing to Pool updates.
// For each remote fixed paths can be set. Otherwise paths are received from pool updates or received packets.
// By default the MultiReplySelector will reply via the path the packet was received on.
type MultiReplySelector struct {
	mtx        sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
	ticker     *time.Ticker
	useUpdates bool

	Remotes     map[UdpAddrKey]RemoteEntry `json:"remotes"`
	RemotesPath map[UdpAddrKey]*Path       `json:"remotes_path"`
	IaRemotes   map[addr.IA][]UdpAddrKey   `json:"ia_remotes"`
	IaPaths     map[addr.IA][]*Path        `json:"ia_paths"`
}

var (
	_ ReplySelector = &MultiReplySelector{}
)

func NewMultiReplySelector(ctx context.Context) *MultiReplySelector {
	rCtx, rCancel := context.WithCancel(ctx)
	selector := &MultiReplySelector{
		ctx:         rCtx,
		cancel:      rCancel,
		ticker:      time.NewTicker(10 * time.Second),
		useUpdates:  true,
		Remotes:     make(map[UdpAddrKey]RemoteEntry),
		RemotesPath: make(map[UdpAddrKey]*Path),
		IaRemotes:   make(map[addr.IA][]UdpAddrKey),
		IaPaths:     make(map[addr.IA][]*Path),
	}

	return selector
}

func (s *MultiReplySelector) UpdateRemoteCwnd(addr UDPAddr, cwnd uint64) {
	ukey := makeKey(addr)
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	rEntry, ok := s.Remotes[ukey]
	if !ok {
		log.Printf("CWND Update: Unkown remote with key: %s\n", ukey.String())
		return
	}
	currentPath := rEntry.paths[0]
	if rEntry.fixedPath != nil {
		log.Println("CWND Updater: using fixed path")
		currentPath = rEntry.fixedPath
	}
	log.Printf("Register CWND %d on path: %s\n", cwnd, fmt.Sprintf("%s %s %s", currentPath.Source, currentPath.Destination, currentPath.Fingerprint))
	go stats.RegisterCwnd(currentPath, cwnd)
}

func (s *MultiReplySelector) RemoteClients() []UdpAddrKey {
	clients := make([]UdpAddrKey, len(s.Remotes))
	for addrKey, _ := range s.Remotes {
		clients = append(clients, addrKey)
	}

	return clients
}

func (s *MultiReplySelector) OnPathDown(PathFingerprint, PathInterface) {
	log.Println("PathDown event missed/ignored in DefaultReplySelector")
}

func (s *MultiReplySelector) SetFixedPath(dst UDPAddr, path *Path) {
	ukey := makeKey(dst)

	if path == nil {
		log.Println("Trying to set fixed path which is NIL!")
		return
	}

	s.mtx.Lock()
	defer s.mtx.Unlock()
	r, ok := s.Remotes[ukey]
	if !ok {
		log.Printf("No remote for key %s found\n", ukey.String())
		return
	}
	r.SetFixedPath(path)
	s.Remotes[ukey] = r
	log.Printf("Set path %s for %s\n", path.String(), ukey.String())
}

func (s *MultiReplySelector) ClearFixedPath(dst UDPAddr) {
	ukey := makeKey(dst)
	s.mtx.Lock()
	defer s.mtx.Unlock()
	r, ok := s.Remotes[ukey]
	if !ok {
		log.Printf("No remote for key %s found\n", ukey.String())
	}
	r.ClearFixedPath()
	s.Remotes[ukey] = r
}

func (s *MultiReplySelector) PathFromElement(dst UDPAddr, pElem string) *Path {
	ukey := makeKey(dst)
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	remotePaths := s.Remotes[ukey].paths
	iaPaths := s.IaPaths[ukey.IA]

	splitted := strings.Split(pElem, ">")
	if len(splitted) != 2 {
		log.Fatalf("Error splitting %s into %v", pElem, splitted)
	}
	bottleneckEgressInterface, err := PathInterfaceFromString(splitted[0])
	if err != nil {
		log.Printf("PathInterface error: %v", err)
		return nil
	}
	bottleneckIngressInterface, err := PathInterfaceFromString(splitted[1])
	if err != nil {
		log.Printf("PathInterface error: %v", err)
		return nil
	}

	for _, rPath := range remotePaths {
		if IsInterfaceOnPath(rPath, bottleneckEgressInterface) && IsInterfaceOnPath(rPath, bottleneckIngressInterface) {
			return rPath
		}
	}

	for _, rPath := range iaPaths {
		if IsInterfaceOnPath(rPath, bottleneckEgressInterface) && IsInterfaceOnPath(rPath, bottleneckIngressInterface) {
			return rPath
		}
	}

	return nil
}

func (s *MultiReplySelector) ReplyPath(src, dst UDPAddr) *Path {
	ukey := makeKey(dst)
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	r, ok := s.Remotes[ukey]
	if !ok {
		log.Println("Reply got unknown destination!!!!")
		return nil
	}

	var rPath *Path
	if r.fixedPath != nil {
		rPath = r.fixedPath
	} else {
		rPath = r.paths[0]
	}

	// only start this go routine which registers the path in the remotes map if it isn't present already
	if s.RemotesPath[ukey] != rPath {
		go func(p *Path, remoteDest UdpAddrKey, panStats *pathStatsDB) {
			s.mtx.Lock()
			defer s.mtx.Unlock()
			s.RemotesPath[ukey] = p
			panStats.RegisterPath(p)
			p.FetchMetadata()
		}(rPath, ukey, &stats)
	}
	return rPath
}

// updateRemotes keeps track of the available paths for a given remote UDPAddr
func (s *MultiReplySelector) updateRemotes(src, dst UDPAddr, path *Path) {
	if path == nil {
		return
	}

	ksrc := makeKey(src)
	s.mtx.Lock()
	defer s.mtx.Unlock()

	r, ok := s.Remotes[ksrc]
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
			remote, ok := s.Remotes[ksrc]
			s.mtx.Unlock()
			if !ok {
				// remote was already removed!
				return
			}
			select {
			case <-remote.expireTimer.C:
				s.mtx.Lock()
				defer s.mtx.Unlock()
				delete(s.Remotes, ksrc)

				// remove this remote from the s.iaRemotes list
				if remotes, ok := s.IaRemotes[ksrc.IA]; ok {
					for i, rem := range remotes {
						if rem == ksrc {
							lr := len(remotes)
							if lr > 1 {
								// replace item to delete with last item in list and cut last item off
								remotes[i] = remotes[lr-1]
								remotes = remotes[:lr-1]
							} else {
								// there is only one item so set empty slice
								remotes = []UdpAddrKey{}
							}

							s.IaRemotes[ksrc.IA] = remotes
						}
					}
				}

				log.Printf("Deleting %s from remotes after expiry\n", ksrc.String())
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
	s.Remotes[ksrc] = r
}

// updateIA keeps track of the open remotes for an IA contained in the UDPAddr
func (s *MultiReplySelector) updateIA(src, dst UDPAddr, path *Path) {
	if path == nil {
		return
	}
	kSrc := makeKey(src)

	s.mtx.Lock()
	defer s.mtx.Unlock()
	if _, ok := s.IaRemotes[kSrc.IA]; !ok && s.useUpdates {
		// we got a new IA -> subscribe for updates
		paths, err := pool.subscribe(s.ctx, kSrc.IA, s)
		if err != nil {
			log.Printf("Error subscribing to pool updates for %s\n", kSrc.IA.String())
			os.Exit(-1)
		}

		go func() {
			// register new paths to destination IA with pathDB
			for _, p := range paths {
				stats.RegisterPath(p)
			}
		}()

		s.IaPaths[kSrc.IA] = paths
	}
	if _, ok := s.Remotes[kSrc]; !ok {
		// we got a new remote we have to add to our IA to remotes mapping
		s.IaRemotes[kSrc.IA] = append(s.IaRemotes[kSrc.IA], kSrc)
	}
}

func (s *MultiReplySelector) AskPathChanges() ([]UDPAddr, bool) {
	if len(s.Remotes) < 1 {
		return []UDPAddr{}, false
	}
	log.Print("Do you want to perform path selection for remotes? [y/N]: ")
	scanner := bufio.NewScanner(os.Stdin)

	scanner.Scan()
	choice := scanner.Text()
	if choice != "yes" && choice != "y" {
		return []UDPAddr{}, false
	}
	remotes, err := s.chooseRemoteInteractive()
	if err != nil {
		log.Printf("Error choosing remote: %v \n", err)
		return []UDPAddr{}, false
	}
	path, err := s.choosePathInteractive(remotes)
	if err != nil {
		log.Printf("Error choosing path: %v \n", err)
		return []UDPAddr{}, false
	}

	remoteAddrs := make([]UDPAddr, len(remotes))
	for i, remote := range remotes {
		s.SetFixedPath(remote.ToUDPAddr(), path)
		remoteAddrs[i] = remote.ToUDPAddr()
	}
	return remoteAddrs, true
}

func (s *MultiReplySelector) chooseRemoteInteractive() ([]*UdpAddrKey, error) {
	log.Printf("Available remotes: \n")
	indexToRemote := make([]UdpAddrKey, len(s.Remotes))
	i := 0
	for remote, _ := range s.Remotes {
		log.Printf("[%2d] %s\n", i, remote.String())
		indexToRemote[i] = remote
		i++
	}

	scanner := bufio.NewScanner(os.Stdin)
	log.Printf("Choose remote: ")
	scanner.Scan()
	remoteIndexStr := scanner.Text()
	remoteIndexStr = strings.ReplaceAll(remoteIndexStr, " ", "")
	indices := []int{}
	indicesStr := strings.Split(remoteIndexStr, ",")
	if len(indicesStr) < 1 {
		return nil, fmt.Errorf("received malformed indice string %v", remoteIndexStr)
	}

	for _, index := range indicesStr {
		idx, err := strconv.Atoi(index)
		if err != nil {
			return nil, fmt.Errorf("invalid remote index string %v. Can not convert to int", index)
		}
		indices = append(indices, idx)
	}

	selectedRemotes := make([]*UdpAddrKey, 0, len(indices))
	for _, idx := range indices {
		if 0 <= idx && idx < len(s.Remotes) {
			selectedRemotes = append(selectedRemotes, &indexToRemote[idx])
		} else {
			fmt.Printf("Invalid remote index %v, valid indices range: [0, %v]. Ignoring it.\n", idx, len(s.Remotes)-1)
		}
	}

	return selectedRemotes, nil
}

func (s *MultiReplySelector) choosePathInteractive(remotes []*UdpAddrKey) (path *Path, err error) {

	exampleRemote := remotes[0]
	for _, remote := range remotes {
		if remote.IA != exampleRemote.IA {
			return nil, fmt.Errorf("all remotes must be located in the same IA, but got %s != %s", exampleRemote.IA.String(), remote.IA.String())
		}
	}

	paths := s.IaPaths[exampleRemote.IA]

	log.Printf("Available paths to IA: %s\n", exampleRemote.IA.String())
	for i, path := range paths {
		log.Printf("[%2d] %s\n", i, fmt.Sprintf("%s", path))
	}

	var selectedPath *Path
	scanner := bufio.NewScanner(os.Stdin)
	log.Printf("Choose path: ")
	scanner.Scan()
	pathIndexStr := scanner.Text()
	pathIndex, err := strconv.Atoi(pathIndexStr)
	if err == nil && 0 <= pathIndex && pathIndex < len(paths) {
		selectedPath = paths[pathIndex]
	} else {
		return nil, fmt.Errorf("Invalid path index %v, valid indices range: [0, %v]\n", pathIndex, len(paths)-1)
	}

	re := regexp.MustCompile(`\d{1,4}-([0-9a-f]{1,4}:){2}[0-9a-f]{1,4}`)
	log.Printf("Using path:\n %s\n", re.ReplaceAllStringFunc(fmt.Sprintf("%s", selectedPath), color.Cyan))
	return selectedPath, nil
}

func (s *MultiReplySelector) Close() error {
	s.cancel()
	return nil
}

// Export is used by stats exporters to lock the selector to be able to export it as JSON file
func (s *MultiReplySelector) Export() ([]byte, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	for remoteKey, path := range s.RemotesPath {
		path.FetchMetadata()
		s.RemotesPath[remoteKey] = path
	}

	return json.Marshal(s)
}

func (s *MultiReplySelector) refresh(dst addr.IA, paths []*Path) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	log.Printf("Received update for %s\n", dst.String())
	s.IaPaths[dst] = paths
}

func (s *MultiReplySelector) OnPacketReceived(src, dst UDPAddr, path *Path) {
	s.updateIA(src, dst, path)
	s.updateRemotes(src, dst, path)
}

func (s *MultiReplySelector) ActiveRemotes() {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	var sb strings.Builder
	if len(s.IaRemotes) == 0 {
		sb.WriteString("No active connections.")
	}

	for _, val := range s.IaRemotes {
		for _, rem := range val {
			path := s.ReplyPath(UDPAddr{}, rem.ToUDPAddr())
			if path.Metadata == nil {
				go path.FetchMetadata()
			}

			sb.WriteString(fmt.Sprintf("%s on path: %s \n", rem.String(), path.String()))
		}
	}
	log.Println(sb.String())
}

func (s *MultiReplySelector) AvailablePaths() string {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	var sb strings.Builder
	for key, val := range s.IaPaths {
		sb.WriteString(fmt.Sprintf("%s ", key.String()))
		for _, path := range val {
			path.FetchMetadata()
			sb.WriteString(fmt.Sprintf("%s \n", path.String()))
		}
	}
	return sb.String()
}

func (u *UdpAddrKey) String() string {
	return fmt.Sprintf("%s,[%s:%d]", u.IA.String(), net.IP(u.IP[:]).String(), u.Port)
}

func (u *UdpAddrKey) ToUDPAddr() UDPAddr {
	return UDPAddr{
		IA:   u.IA,
		IP:   u.IP[:],
		Port: u.Port,
	}
}

func (u UdpAddrKey) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.String())
}

func (u UdpAddrKey) MarshalText() ([]byte, error) {
	return []byte(u.String()), nil
}

func makeKey(a UDPAddr) UdpAddrKey {
	k := UdpAddrKey{
		IA:   a.IA,
		Port: a.Port,
	}
	copy(k.IP[:], a.IP.To16())
	return k
}

func (u *RemoteEntry) MarshalJSON() ([]byte, error) {
	s := struct {
		Paths int `json:"paths_used"`
		Seen  int `json:"last_seen"`
	}{
		Paths: len(u.paths),
		Seen:  int(u.seen.Unix()),
	}
	return json.Marshal(s)
}

func (u *RemoteEntry) MarshalText() ([]byte, error) {
	return []byte(fmt.Sprintf("paths:%d,seen:%d", len(u.paths), int(u.seen.Unix()))), nil
}

func (u *RemoteEntry) SetFixedPath(p *Path) {
	u.fixedPath = p
}

func (u *RemoteEntry) ClearFixedPath() {
	u.fixedPath = nil
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

//func (p *pathsMRU) string() string {
//	var sb strings.Builder
//	for _, path := range *p {
//		path.FetchMetadata()
//		sb.WriteString(path.String())
//	}
//	return sb.String()
//}
