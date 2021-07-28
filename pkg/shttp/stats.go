package shttp

import (
	"fmt"
	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/http3"
	"github.com/scionproto/scion/go/lib/log"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	clientTimeout = 30 * time.Second
)

// SHTTPStats implements the HTTPStats interface to track HTTP requests of multiple clients.
// Due to the concurrent requests we have to use the mtx to allow safe concurrent access to the internal data structures.
// The exported functions are responsible for acquiring the mutex as well as releasing it again.
type SHTTPStats struct {
	mtx sync.RWMutex

	clients    map[quic.StatsClientID]*http3.StatusEntry
	oldToNewID map[quic.StatsClientID]quic.StatsClientID
}

func NewSHTTPStats() http3.HTTPStats {
	return &SHTTPStats{
		clients:    make(map[quic.StatsClientID]*http3.StatusEntry),
		oldToNewID: make(map[quic.StatsClientID]quic.StatsClientID),
	}
}

func (s *SHTTPStats) getCurrentClientID(cID quic.StatsClientID) (quic.StatsClientID, error) {
	_, ok := s.clients[cID]
	if !ok {
		newCID, ok := s.oldToNewID[cID]
		if !ok {
			return "", fmt.Errorf("%s is not a current ID", cID)
		}
		return newCID, nil
	}
	return cID, nil
}

func (s *SHTTPStats) migrateToNewClientID(oldID, newID quic.StatsClientID) error {
	cEntry, ok := s.clients[oldID]
	if !ok {
		return fmt.Errorf("%s is unkown can't migrate", oldID)
	}

	_, ok = s.clients[newID]
	if ok {
		return fmt.Errorf("can't migrate to pre-existing id: %s", newID)
	}

	cEntry.ClientID = newID
	s.clients[newID] = cEntry
	s.oldToNewID[oldID] = newID
	delete(s.clients, oldID)
	return nil
}

func (s *SHTTPStats) retireAfterInactivity(cID quic.StatsClientID) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
loop:
	for {
		select {
		case <-t.C:
			s.mtx.Lock()
			currentCid, err := s.getCurrentClientID(cID)
			if err != nil {
				log.Error(fmt.Sprintf("%v", err))
				break loop
			}
			client, ok := s.clients[currentCid]
			if ok {
				if client.LastUpdate.Before(time.Now().Add(-clientTimeout)) {
					s.clients[currentCid].Status = http3.Inactive
					break loop
				}
			} else {
				log.Error(fmt.Sprintf("Couldn't set %s to inactive because client does not exist.", cID))
				break loop
			}
			s.mtx.Unlock()
		default:
			time.Sleep(1 * time.Second)
		}
	}
	s.mtx.Unlock()
}

func (s *SHTTPStats) updateStatus(cID quic.StatsClientID, new http3.Status) error {
	current := s.clients[cID].Status

	if current != new && new == http3.Alive {
		// if we change the status back to Alive we have to restart the retire routine
		go s.retireAfterInactivity(cID)
	}
	s.clients[cID].Status = new
	return nil
}

func (s *SHTTPStats) addClient(cID quic.StatsClientID, sess quic.Session) error {
	_, ok := s.clients[cID]
	if ok {
		return fmt.Errorf("%s already registered as client", cID)
	}
	s.clients[cID] = http3.NewStatusEntry(cID, sess.RemoteAddr(), sess, http3.Alive)
	go s.retireAfterInactivity(cID)
	return nil
}

func (s *SHTTPStats) AddClient(cID quic.StatsClientID, sess quic.Session) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	check(s.addClient(cID, sess))
}

func (s *SHTTPStats) retireClient(cID quic.StatsClientID) error {
	check(s.updateStatus(cID, http3.Retired))
	s.clients[cID].Updated()
	return nil
}

func (s *SHTTPStats) RetireClient(cID quic.StatsClientID) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	cID, err := s.getCurrentClientID(cID)
	check(err)
	check(s.retireClient(cID))
}

func (s *SHTTPStats) addFlow(cID quic.StatsClientID) error {
	s.clients[cID].Flows++
	s.clients[cID].Updated()
	return nil
}

func (s *SHTTPStats) AddFlow(cID quic.StatsClientID) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	cID, err := s.getCurrentClientID(cID)
	check(err)
	check(s.addFlow(cID))
}

func (s *SHTTPStats) removeFlow(cID quic.StatsClientID) error {
	s.clients[cID].Flows--
	s.clients[cID].Updated()
	return nil
}

func (s *SHTTPStats) RemoveFlow(cID quic.StatsClientID) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	cID, err := s.getCurrentClientID(cID)
	check(err)
	check(s.removeFlow(cID))
}

func (s *SHTTPStats) NotifyChanged(oldID, newID quic.StatsClientID) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	check(s.migrateToNewClientID(oldID, newID))
}

func (s *SHTTPStats) lastRequest(cID quic.StatsClientID, r string) error {
	if !(strings.HasSuffix(r, ".m4v") || strings.HasSuffix(r, ".mp4")) {
		return nil
	}

	s.clients[cID].LastRequest = r
	check(s.updateStatus(cID, http3.Alive))
	s.clients[cID].Updated()
	return nil
}

func (s *SHTTPStats) LastRequest(cID quic.StatsClientID, r string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	cID, err := s.getCurrentClientID(cID)
	check(err)
	check(s.lastRequest(cID, r))
}

func (s *SHTTPStats) lastCwnd(cID quic.StatsClientID, c int) error {
	s.clients[cID].LastCwnd = c
	s.clients[cID].Updated()
	return nil
}

func (s *SHTTPStats) LastCwnd(cID quic.StatsClientID, c int) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	cID, err := s.getCurrentClientID(cID)
	check(err)
	check(s.lastCwnd(cID, c))
}

func (s *SHTTPStats) All() []*http3.StatusEntry {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	all := make([]*http3.StatusEntry, 0, len(s.clients))
	for _, val := range s.clients {
		all = append(all, val)
	}

	return all
}

func check(err error) {
	if err != nil {
		log.Error(fmt.Sprintf("SHTTP_ERROR: %v", err))
		panic(fmt.Sprintf("SHTTP_ERROR: %v \n", err))
	}
}

func SetupLogger() {
	logCfg := log.Config{Console: log.ConsoleConfig{Level: "debug"}}
	if err := log.Setup(logCfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error configuring logger. Exiting due to:%s\n", err)
		os.Exit(-1)
	}
}
