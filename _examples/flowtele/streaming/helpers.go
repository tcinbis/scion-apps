package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"time"

	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/http3"
	"github.com/netsec-ethz/scion-apps/pkg/pan"
	"github.com/netsec-ethz/scion-apps/pkg/shttp"
)

func CWNDPathExplorerHelper(selector *pan.MultiReplySelector, serverStats *shttp.SHTTPStats) {
	go func() {
		var clients []pan.UdpAddrKey
		for {
			time.Sleep(30 * time.Second)
			clients = selector.RemoteClients()
			for _, p := range pan.PathsWithoutCwnd() {
				for idx, rAddrKey := range clients {
					if p.Destination != rAddrKey.IA {
						// client is not reachable via path p
						continue
					}

					selector.SetFixedPath(rAddrKey.ToUDPAddr(), p)
					if entry := serverStats.GetHTTPStatusByRemoteAddr(rAddrKey.ToUDPAddr()); entry != nil {
						entry.Session.MigrateConnection()
						entry.LastCwnd.Clear()
					}
					// we found a client who's path we change so remove it and move on
					clients = append(clients[:idx], clients[idx+1:]...)
					break
				}
			}
		}
	}()
}

func CWNDUpdateHelper(selector *pan.MultiReplySelector, serverStats *shttp.SHTTPStats) {
	for {
		time.Sleep(5 * time.Second)
		for _, b := range serverStats.All() {
			if b.Status != http3.Alive {
				// skip an entry that is not being updated anymore
				continue
			}

			rAddr, ok := b.Remote.(pan.UDPAddr)
			if !ok {
				fmt.Println("Error casting address to UDPAddr")
				continue
			}
			selector.UpdateRemoteCwnd(rAddr, uint64(b.LastCwnd.Mean()))
		}
		log.Printf("Paths without CWND measurement: %v\n", pan.PathsWithoutCwnd())
	}
}

func InteractivePathsHelper(selector *pan.MultiReplySelector, serverStats *shttp.SHTTPStats) {
	for {
		remotes, ok := selector.AskPathChanges()
		for _, remote := range remotes {
			if ok {
				serverStats.GetSessionByRemoteAddr(remote).MigrateConnection()
			}
		}
		time.Sleep(1 * time.Second)
	}
}

func StatsExporterHelper(selector *pan.MultiReplySelector, serverStats *shttp.SHTTPStats) {
	for {
		statsExporter(serverStats.All(), selector)
		time.Sleep(5 * time.Second)
	}
}

func statsExporter(httpStats []*http3.StatusEntry, panStats *pan.MultiReplySelector) {
	writeJson(httpStats, "http.json")
	writeJson(panStats, "pan.json")
}

func writeJson(obj interface{}, filename string) {
	var res []byte
	var err error

	if eObj, ok := obj.(interface{ Export() ([]byte, error) }); ok {
		res, err = eObj.Export()
	} else {
		res, err = json.MarshalIndent(obj, "", "\t")
	}
	check(err)

	currDir, err := os.Getwd()
	if err != nil {
		log.Println(err)
		os.Exit(-1)
	}

	f, err := os.Create(path.Join(currDir, filename))
	check(err)
	defer f.Close()

	check(f.Truncate(0))
	_, err = f.Seek(0, 0)
	check(err)

	w := bufio.NewWriter(f)
	_, err = w.Write(res)
	check(err)
	w.Flush()
}

func checkSession(sess *quic.EarlySession) *quic.Session {
	qs, ok := (*sess).(quic.Session)
	if !ok {
		fmt.Println("Returned session is not quic sessions")
		return nil
	}
	return &qs
}

func checkFlowTeleSession(sess *quic.Session) *quic.FlowTeleSession {
	fs, ok := (*sess).(quic.FlowTeleSession)
	if !ok {
		fmt.Println("Returned session is not flowtele sessions")
		return nil
	}
	return &fs
}
