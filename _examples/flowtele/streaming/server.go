package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/flowtele"
	"github.com/lucas-clemente/quic-go/http3"
	flowteledbus "github.com/netsec-ethz/scion-apps/_examples/flowtele/dbus"
	"github.com/netsec-ethz/scion-apps/pkg/pan"
	"github.com/netsec-ethz/scion-apps/pkg/shttp"
	"github.com/scionproto/scion/go/lib/addr"
	slog "github.com/scionproto/scion/go/lib/log"
	"gopkg.in/alecthomas/kingpin.v2"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	Bit  = 1
	KBit = 1000 * Bit
	MBit = 1000 * KBit
	GBit = 1000 * MBit

	Byte  = 8 * Bit
	KByte = 1000 * Byte
	MByte = 1000 * KByte
)

var (
	ip                     = kingpin.Flag("ip", "ip to listen on").Default("127.0.0.1").String()
	port                   = kingpin.Flag("port", "port the server listens on").Default("8001").Uint()
	useScion               = kingpin.Flag("scion", "Enable serving server via SCION").Default("false").Bool()
	certDir                = kingpin.Flag("certs", "Path to the certs directory.").Default("").String()
	dataDir                = kingpin.Flag("data", "Path to the data directory.").Default("").String()
	mappingDir             = kingpin.Flag("mapping", "Path to mapping directory.").Default("").String()
	interactivePathChanges = kingpin.Flag("interactive", "Enables interactive path changes via the terminal").Default("false").Bool()
	csvFilePrefix          = kingpin.Flag("csv-prefix", "File prefix to use for writing the CSV file.").Default("rtt").String()
)

var loggerWait sync.WaitGroup

func init() {
	kingpin.Parse()
	fmt.Printf("Parsing complete. Listening on %v:%v\n", *ip, *port)
}

func getTCPConn(addr string) *net.TCPListener {
	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	tcpConn, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		log.Fatal(err)
	}
	return tcpConn
}

func getUDPConn(addr string) *net.UDPConn {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		log.Fatal(err)
	}
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		log.Fatal()
	}
	return udpConn
}

func getQuicConf(stats http3.HTTPStats, loggingPrefix string, localIA, remoteIA addr.IA) *quic.Config {

	dummyFlowteleSignalInterface := flowtele.CreateFlowteleSignalInterface(nil, nil, nil, nil)

	var ob func(oldID, newID quic.StatsClientID)
	if stats != nil {
		ob = stats.NotifyChanged
	}

	var newSessionCallback func(connID string, session quic.FlowTeleSession) error
	if *useScion {
		newSessionCallback = func(connID string, session quic.FlowTeleSession) error {
			fmt.Println("Starting DBUS")
			qdbus := flowteledbus.NewQuicDbus(0, true, connID)
			qdbus.SetMinIntervalForAllSignals(10 * time.Millisecond)
			qdbus.Session = session
			if err := qdbus.OpenSessionBus(); err != nil {
				return err
			}
			defer qdbus.Close()
			if err := qdbus.Register(); err != nil {
				return err
			}
			// we initialized quic with a pointer to a dummy FlowTeleSignalInterface.
			// now that we know the true connectionID we point the pointer to a real interface
			ctx, cancelLoggers := context.WithCancel(context.Background())
			defer cancelLoggers()
			*(dummyFlowteleSignalInterface) = *flowteledbus.GetFlowTeleSignalInterface(
				ctx,
				qdbus,
				connID,
				session.LocalAddr().String(),
				session.RemoteAddr().String(),
				*useScion,
				localIA,
				remoteIA,
				loggingPrefix,
				&loggerWait,
			)
			return nil
		}
	} else {
		newSessionCallback = func(connID string, session quic.FlowTeleSession) error {
			return nil
		}
	}

	return &quic.Config{
		// make QUIC idle timout long to allow a delay between starting the listeners and the senders
		//MaxIdleTimeout: 30 * time.Second,
		//KeepAlive: true,
		FlowTeleSignal:       dummyFlowteleSignalInterface,
		ConnectionIDObserver: ob,
		NewSessionCallback:   newSessionCallback,
	}
}

func startTCPServer(handler http.Handler) {
	fmt.Printf("Using QUIC\n")
	serverAddr := fmt.Sprintf("%s:%d", *ip, *port)

	if *certDir == "" {
		log.Fatal("Cert directory not specified! Can't load certificates.")
	}

	certFile := filepath.Join(*certDir, "/live/colasloth.com/fullchain.pem")
	keyFile := filepath.Join(*certDir, "/live/colasloth.com/privkey.pem")
	certs := make([]tls.Certificate, 1)
	var err error
	certs[0], err = tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		log.Fatal(err)
	}

	tlsConfig := &tls.Config{
		Certificates: certs,
	}

	stats := shttp.NewSHTTPStats()
	quicServer := &http3.Server{
		Server: &http.Server{
			Addr:      serverAddr,
			Handler:   handler,
			TLSConfig: tlsConfig,
		},
		QuicConfig: getQuicConf(stats, *csvFilePrefix, addr.IA{}, addr.IA{}),
		Stats:      stats,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		quicServer.SetQuicHeaders(w.Header())
		w.Header().Add("Access-Control-Allow-Headers", "*")
		w.Header().Add("Access-Control-Allow-Origin", "*")
		w.Header().Add("Access-Control-Allow-Methods", "*")
		handler.ServeHTTP(w, r)
	})

	quicServer.Handler = mux
	quicServer.SetNewStreamCallback(func(sess *quic.EarlySession, strID quic.StreamID) {
		//fmt.Printf("%v: Session to %s open.\n", time.Now(), (*sess).RemoteAddr())
		//(*checkFlowTeleSession(checkSession(sess))).SetFixedRate(2.5 * MBit)
	})

	go func() {
		defer slog.HandlePanic()
		quicServer.Server.Serve(tls.NewListener(getTCPConn(serverAddr), tlsConfig))
	}()
	//quicServer.ListenAndServeTLS(certFile, keyFile)
	go func() {
		defer slog.HandlePanic()
		quicServer.Serve(getUDPConn(serverAddr))
	}()

	for {
		//ids := make([]string, len(*quicServer.GetSessions()))
		//for key, val := range *quicServer.GetSessions() {
		//	ids = append(ids, fmt.Sprintf("ConnectionID: %s - %d\n", key, val))
		//}
		//sort.Strings(ids)
		//for _, s := range ids {
		//	tm.Println(s)
		//}
		allData := quicServer.Stats.All()
		sort.Slice(allData, func(i, j int) bool {
			return allData[i].ClientID > allData[j].ClientID
		})
		if len(allData) > 0 {
			for _, entry := range allData {
				fmt.Println(entry.String())
			}
			fmt.Println()
		}
		time.Sleep(500 * time.Millisecond)
	}

	//http.Handle("/", handler)
	//http.ListenAndServe(fmt.Sprintf("%s:%d", *ip, *port), nil)
}

func startSCIONServer(handler http.Handler) {
	fmt.Printf("Using SCION\n")
	serverAddr := fmt.Sprintf("%s:%d", *ip, *port)

	stats := shttp.NewSHTTPStats()

	udpPacketCon, err := pan.ListenUDP(context.Background(), &net.UDPAddr{Port: int(*port)}, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(udpPacketCon.LocalAddr())

	server := shttp.NewScionServer(serverAddr, handler, nil, getQuicConf(stats, *csvFilePrefix, pan.LocalIA(), addr.IA{}))
	server.Server.Stats = stats
	//server.Server.SetNewStreamCallback(func(sess *quic.EarlySession, strID quic.StreamID) {
	//	fmt.Printf("%v %v\n", time.Now(), sess)
	//	fmt.Printf("%v: Session to %s open.\n", time.Now(), (*sess).RemoteAddr())
	//	fmt.Printf("%v %v\n", time.Now(), server.Server.GetSessions())
	//	//(*checkFlowTeleSession(checkSession(sess))).SetFixedRate(500 * KByte)
	//})

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		server.SetQuicHeaders(w.Header())
		w.Header().Add("Access-Control-Allow-Headers", "*")
		w.Header().Add("Access-Control-Allow-Origin", "*")
		w.Header().Add("Access-Control-Allow-Methods", "*")
		//log.Printf("%s", r.RequestURI)
		handler.ServeHTTP(w, r)
	})

	server.Handler = mux
	go func() {
		defer slog.HandlePanic()
		server.Serve(udpPacketCon)
	}()

	selector, ok := udpPacketCon.GetSelector().(*pan.MultiReplySelector)
	if !ok {
		fmt.Println("Error casting reply selector")
		os.Exit(1)
	}

	serverStats, ok := server.Stats.(*shttp.SHTTPStats)
	if !ok {
		fmt.Println("Error casting HTTP Server stats.")
	}

	go func() {
		for {
			time.Sleep(5 * time.Second)
			for _, b := range serverStats.All() {
				rAddr, ok := b.Remote.(pan.UDPAddr)
				if !ok {
					fmt.Println("Error casting address to UDPAddr")
					continue
				}
				selector.UpdateRemoteCwnd(rAddr, uint64(b.LastCwnd.Mean()))
			}
			fmt.Printf("Paths without CWND measurement: %v\n", pan.PathsWithoutCwnd())
		}
	}()

	go func() {
		for {
			time.Sleep(10 * time.Second)
			for _, p := range pan.PathsWithoutCwnd() {
				for _, rAddrKey := range selector.RemoteClients() {
					if p.Destination != rAddrKey.IA {
						continue
					}
					selector.SetFixedPath(rAddrKey.ToUDPAddr(), p)
					if entry := serverStats.GetHTTPStatusByRemoteAddr(rAddrKey.ToUDPAddr()); entry != nil {
						entry.LastCwnd.Clear()
					}
				}
			}
		}
	}()

	if *interactivePathChanges {
		go func(sel *pan.MultiReplySelector) {
			for {
				remote, ok := sel.AskPathChanges()
				if ok {
					s, ok := server.Stats.(*shttp.SHTTPStats)
					if !ok {
						fmt.Println("Error casting HTTP Server stats.")
					}
					s.GetSessionByRemoteAddr(remote).MigrateConnection()
				}
				time.Sleep(1 * time.Second)
			}
		}(selector)
	}

	go func(sel *pan.MultiReplySelector) {
		for {
			statsExporter(server.Stats.All(), sel)
			time.Sleep(5 * time.Second)
		}
	}(selector)

	initMonitorPanMappings(selector)

	for {
		allData := server.Stats.All()
		sort.Slice(allData, func(i, j int) bool {
			return allData[i].ClientID > allData[j].ClientID
		})
		if len(allData) > 0 {
			for _, entry := range allData {
				fmt.Println(entry.String())
				//sess := checkFlowTeleSession(&entry.Session)
				//(*sess).SetFixedRate(1000 * KBit)
			}
			fmt.Println()
		}
		time.Sleep(1 * time.Second)
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

func initMonitorPanMappings(sel *pan.MultiReplySelector) {
	callback := func(s string) {
		handleNewPanPath(sel, s)
	}
	if *mappingDir == "" {
		fmt.Println("Can't init file watch without mapping dir!")
		return
	}
	configureFileWatch(*mappingDir, "pan-paths.json", callback)
}

func configureFileWatch(watchDir, filename string, writeCallback func(string)) {
	// creates a new file watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Println("ERROR", err)
	}
	defer watcher.Close()
	done := make(chan bool)

	go func() {
		for {
			select {
			case event := <-watcher.Events:
				if strings.HasSuffix(event.Name, filename) {
					if event.Op&fsnotify.Write == fsnotify.Write {
						log.Println("modified file:", event.Name)
						writeCallback(event.Name)
					}
				}
			case err := <-watcher.Errors:
				fmt.Println("ERROR", err)
			}
		}
	}()

	// out of the box fsnotify can watch a single file, or a single directory
	if err := watcher.Add(watchDir); err != nil {
		fmt.Println("ERROR", err)
	}
	time.Sleep(100 * time.Second)
	<-done

}

func main() {
	shttp.SetupLogger()
	filePath := filepath.Join(*dataDir)
	fmt.Println(filePath)
	handler := http.FileServer(http.Dir(filePath))
	if *useScion {
		startSCIONServer(handler)
	} else {
		startTCPServer(handler)
	}

}

// withLogger returns a handler that logs requests (after completion) in a simple format:
//	  <time> <remote address> "<request>" <status code> <size of reply>
func withLogger(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrec := &recordingResponseWriter{ResponseWriter: w}
		h.ServeHTTP(wrec, r)

		log.Printf("%s \"%s %s %s/SCION\" %d %d\n",
			r.RemoteAddr,
			r.Method, r.URL, r.Proto,
			wrec.status, wrec.bytes)
	})
}

type recordingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *recordingResponseWriter) WriteHeader(statusCode int) {
	r.status = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *recordingResponseWriter) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	r.bytes += len(b)
	return r.ResponseWriter.Write(b)
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

// Check just ensures the error is nil, or complains and quits
func check(e error) {
	if e != nil {
		fmt.Fprintln(os.Stderr, "Fatal error:", e)
		os.Exit(1)
	}
}

func handleNewPanPath(sel *pan.MultiReplySelector, filepath string) {
	mapping, err := parsePanPaths(filepath)
	if err != nil {
		check(err)
	}
	for _, elem := range mapping {
		fmt.Println(elem)
		path := sel.PathFromElement(elem.Remote, elem.PathElement)
		if path == nil {
			log.Printf("Couldn't find path for remote: %s and path element: %s", elem.Remote.String(), elem.PathElement)
			continue
		}
		sel.SetFixedPath(elem.Remote, path)
	}
}

type PanMapping struct {
	Remote      pan.UDPAddr `json:"remote"`
	PathElement string      `json:"path_element"`
}

func parsePanPaths(filename string) ([]PanMapping, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("error opening %s: %v", filename, err)
	}
	byteJson, _ := io.ReadAll(file)
	err = file.Close()
	check(err)
	err = os.Remove(filename)
	if err != nil {
		log.Printf("Error deleting %s: %v\n", filename, err)
	}

	var mapping []PanMapping
	return mapping, json.Unmarshal(byteJson, &mapping)
}
