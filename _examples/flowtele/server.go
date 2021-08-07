package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/flowtele"
	"github.com/lucas-clemente/quic-go/http3"
	"github.com/netsec-ethz/scion-apps/pkg/pan"
	"github.com/netsec-ethz/scion-apps/pkg/shttp"
	slog "github.com/scionproto/scion/go/lib/log"
	"gopkg.in/alecthomas/kingpin.v2"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
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
	ip       = kingpin.Flag("ip", "ip to listen on").Default("127.0.0.1").String()
	port     = kingpin.Flag("port", "port the server listens on").Default("8001").Uint()
	useScion = kingpin.Flag("scion", "Enable serving server via SCION").Default("false").Bool()
	certDir  = kingpin.Flag("certs", "Path to the certs directory.").Default("").String()
	dataDir  = kingpin.Flag("data", "Path to the data directory.").Default("").String()
)

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

func getQuicConf(stats http3.HTTPStats) *quic.Config {

	flowteleSignalInterface := flowtele.CreateFlowteleSignalInterface(func(t time.Time, srtt time.Duration) {
		//fmt.Printf("t: %v, srtt: %v\n", t, srtt)
	}, func(t time.Time, newSlowStartThreshold uint64) {

	}, func(t time.Time, lostRatio float64) {
		fmt.Println(lostRatio)
	}, func(t time.Time, congestionWindow uint64, packetsInFlight uint64, ackedBytes uint64) {

	})

	var ob func(oldID, newID quic.StatsClientID)
	if stats != nil {
		ob = stats.NotifyChanged
	}

	return &quic.Config{
		// make QUIC idle timout long to allow a delay between starting the listeners and the senders
		//MaxIdleTimeout: 30 * time.Second,
		//KeepAlive: true,
		FlowTeleSignal:       flowteleSignalInterface,
		ConnectionIDObserver: ob,
	}
}

func startTCPServer(handler http.Handler) {
	fmt.Printf("Using QUIC\n")
	addr := fmt.Sprintf("%s:%d", *ip, *port)

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
			Addr:      addr,
			Handler:   handler,
			TLSConfig: tlsConfig,
		},
		QuicConfig: getQuicConf(stats),
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
		quicServer.Server.Serve(tls.NewListener(getTCPConn(addr), tlsConfig))
	}()
	//quicServer.ListenAndServeTLS(certFile, keyFile)
	go func() {
		defer slog.HandlePanic()
		quicServer.Serve(getUDPConn(addr))
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
	addr := fmt.Sprintf("%s:%d", *ip, *port)

	stats := shttp.NewSHTTPStats()
	server := shttp.NewScionServer(addr, handler, nil, getQuicConf(stats))
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
	udpPacketCon, err := pan.ListenUDP(context.Background(), &net.UDPAddr{Port: int(*port)}, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(udpPacketCon.LocalAddr())
	go func() {
		defer slog.HandlePanic()
		server.Serve(udpPacketCon)
	}()

	//selector, ok := udpPacketCon.GetSelector().(*pan.MultiReplySelector)
	//if !ok {
	//	fmt.Println("Error casting reply selector")
	//	os.Exit(1)
	//}

	//go func(sel *pan.MultiReplySelector) {
	//	for {
	//		remote, ok := sel.AskPathChanges()
	//		if ok {
	//			s, ok := server.Stats.(*shttp.SHTTPStats)
	//			if !ok {
	//				fmt.Println("Error casting HTTP Server stats.")
	//			}
	//			s.GetSessionByRemoteAddr(remote).MigrateConnection()
	//		}
	//		time.Sleep(1 * time.Second)
	//	}
	//}(selector)
	//
	//go func(sel *pan.MultiReplySelector) {
	//	for {
	//		statsExporter(server.Stats.All(), sel)
	//		time.Sleep(5 * time.Second)
	//	}
	//}(selector)

	for {
		//allData := server.Stats.All()
		//sort.Slice(allData, func(i, j int) bool {
		//	return allData[i].ClientID > allData[j].ClientID
		//})
		//if len(allData) > 0 {
		//	for _, entry := range allData {
		//		fmt.Println(entry.String())
		//		//sess := checkFlowTeleSession(&entry.Session)
		//		//(*sess).SetFixedRate(1000 * KBit)
		//	}
		//	fmt.Println()
		//}
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
	f, err := os.Create(path.Join("/home/tom/go/src/scion-apps/_examples/flowtele/", filename))
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
