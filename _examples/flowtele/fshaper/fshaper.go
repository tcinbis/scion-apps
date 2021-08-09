package main

import (
	"flag"
	"fmt"
	"github.com/lucas-clemente/quic-go"
	flowteledbus "github.com/netsec-ethz/scion-apps/_examples/flowtele/dbus"
	"github.com/netsec-ethz/scion-apps/_examples/flowtele/utils"
	"github.com/pkg/profile"
	"github.com/scionproto/scion/go/lib/addr"
	"github.com/scionproto/scion/go/lib/log"
	"gopkg.in/alecthomas/kingpin.v2"
	"net"
	"os"
	"strings"
	"sync"
)

const (
	errorNoError quic.ApplicationErrorCode = 0x100
)

var (
	localIAFromFlag addr.IA

	remoteIpFlag       = kingpin.Flag("ip", "IP address to connect to").Default("127.0.0.1").String()
	remotePortFlag     = kingpin.Flag("port", "Port number to connect to").Default("51000").Int()
	useRemotePortRange = kingpin.Flag("port-range", "Use increasing (remote) port numbers for additional QUIC senders").Default("false").Bool()
	localIpFlag        = kingpin.Flag("local-ip", "IP address to listen on (required for SCION)").Default("").String()
	localPortFlag      = kingpin.Flag("local-port", "Port number to listen on (required for SCION)").Default("51000").Int()
	useLocalPortRange  = kingpin.Flag("local-port-range", "Use increasing local port numbers for additional QUIC senders").Default("true").Bool()
	quicSenderOnly     = kingpin.Flag("quic-sender-only", "Only start the quic sender").Default("false").Bool()
	fshaperOnly        = kingpin.Flag("fshaper-only", "Only start the fshaper").Default("false").Bool()
	quicDbusIndex      = kingpin.Flag("quic-dbus-index", "index of the quic sender dbus name").Default("0").Int()
	nConnections       = kingpin.Flag("num", "Number of QUIC connections").Default("2").Int()
	noApplyControl     = kingpin.Flag("no-apply-control", "Do not forward apply-control calls from fshaper to this QUIC connection (useful to ensure the calibrator flow is not influenced by vAlloc)").Default("false").Bool()
	mode               = kingpin.Flag("mode", "the sockets mode of operation: fetch, quic, fshaper").Default("fshaper").String()
	maxData            = kingpin.Flag("max-data", "the maximum amount of data that should be transmitted on each QUIC flow (0 means no limit)").Default("0").Int()

	useScion     = kingpin.Flag("scion", "Open scion quic sockets").Default("false").Bool()
	localIAFlag  = kingpin.Flag("local-ia", "ISD-AS address to listen on.").String()
	remoteIAFlag = kingpin.Flag("remote-ia", "ISD-AS address to connect to.").String()
	profiling    = kingpin.Flag("profiling", "").Default("false").Bool()
	target       = kingpin.Flag("target", "Convenience flag to interpret joint IP and/or IA addresses. Example: 1.1.1.1 or 16-ffaa:0:1002,1.1.1.1").Default("").String()
)

var (
	sigs       = make(chan os.Signal, 1)
	done       = make(chan bool, 1)
	loggerWait sync.WaitGroup
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

func init() {
	utils.SetupLogger()
	kingpin.Parse()
	localIAFromFlag = *utils.SetAddrIA(*localIAFlag)
	log.Debug(fmt.Sprintf("LocalIA %v\n", localIAFromFlag))

	if len(*target) > 0 {
		log.Info(*target)
		if strings.Contains(*target, "/") {
			// systemd-escape weirdly escapes a - so we have to fix it here
			*target = strings.ReplaceAll(*target, "/", "-")
		}
		log.Info(*target)
		x := strings.Split(*target, ",")
		log.Debug(fmt.Sprintf("target split into: %v\n", x))
		if *useScion {
			// we have to parse IP and IA from target
			if len(x) != 2 {
				log.Error(fmt.Sprintf("Expected target to be separable by comma, but got %v from %v\n", x, *target))
				os.Exit(-1)
			}
			*remoteIAFlag = x[0]
			*remoteIpFlag = x[1]
		} else {
			// only parse IP from target
			if len(x) == 1 {
				*remoteIpFlag = x[0]
			} else if len(x) == 2 {
				*remoteIpFlag = x[1]
			} else {
				log.Error(fmt.Sprintf("Expected target to be single IP, but got %v from %v\n", x, *target))
				os.Exit(-1)
			}

		}
	}
}

func main() {
	// first run
	// python3.6 athena_m2.py 2
	// clear; go run go/flowtele/quic_listener.go --num 3
	// clear; go run go/flowtele/socket.go --fshaper-only
	// clear; go run go/flowtele/socket.go --quic-sender-only --ip 164.90.176.95 --port 5500 --quic-dbus-index 0
	// clear; go run go/flowtele/socket.go --quic-sender-only --ip 164.90.176.95 --port 5501 --quic-dbus-index 1
	// clear; go run go/flowtele/socket.go --quic-sender-only --ip 164.90.176.95 --port 5502 --quic-dbus-index 2
	// can add --no-apply-control to calibrator flow

	// ./scion.sh topology -c topology/Tiny.topo
	// ./scion.sh start
	// bazel build //... && bazel-bin/go/flowtele/listener/linux_amd64_stripped/flowtele_listener --scion --sciond 127.0.0.12:30255 --local-ia 1-ff00:0:110 --num 2
	// bazel build //... && bazel-bin/go/flowtele/linux_amd64_stripped/flowtele_socket --quic-sender-only --scion --sciond 127.0.0.19:30255 --local-ip 127.0.0.1 --local-port 6000 --ip 127.0.0.1 --port 5500 --local-ia 1-ff00:0:111 --remote-ia 1-ff00:0:110 --path 1-ff00:0:111,1-ff00:0:110
	// bazel build //... && bazel-bin/go/flowtele/linux_amd64_stripped/flowtele_socket --quic-sender-only --scion --sciond 127.0.0.19:30255 --local-ip 127.0.0.1 --local-port 6001 --ip 127.0.0.1 --port 5501 --local-ia 1-ff00:0:111 --remote-ia 1-ff00:0:110 --path 1-ff00:0:111,1-ff00:0:110
	errChannel := make(chan error)
	closeChannel := make(chan struct{})
	if *profiling {
		p := profile.Start(profile.CPUProfile, profile.ProfilePath("."), profile.NoShutdownHook)
		defer p.Stop()
	}
	log.Info("Starting...")
	if *fshaperOnly || *mode == "fshaper" {
		log.Info("FShaper\n")
		go func(cc chan struct{}, ec chan error) {
			defer log.HandlePanic()
			invokeFshaper(cc, ec)
		}(closeChannel, errChannel)
	} else {
		flag.PrintDefaults()
		errChannel <- fmt.Errorf("Must provide --fshaper-only")
	}

	select {
	case err := <-errChannel:
		log.Error(fmt.Sprintf("Error encountered (%s), exiting socket\n", err))
		log.Info("Waiting for data loggers to exit")
		loggerWait.Wait()
		log.Info("Data loggers exited. Returning and exiting.")
		return
	case <-closeChannel:
		log.Info("Exiting without errors")
	}
}

func invokeFshaper(closeChannel chan struct{}, errChannel chan error) {
	remoteAddr := net.UDPAddr{IP: net.ParseIP(*remoteIpFlag), Port: *remotePortFlag}
	peerString := utils.CleanStringForFS(remoteAddr.String())
	fdbus := flowteledbus.NewFshaperDbus(*nConnections, peerString)

	// if a min interval for the fshaper is specified, make sure to accumulate acked bytes that would otherwise not be registered by athena
	// fdbus.SetMinIntervalForAllSignals(5 * time.Millisecond)

	// dbus setup
	if err := fdbus.OpenSessionBus(); err != nil {
		errChannel <- err
		return
	}

	// register method and listeners
	if err := fdbus.Register(); err != nil {
		errChannel <- err
		return
	}

	// listen for feedback from QUIC instances and forward to athena
	go func() {
		defer log.HandlePanic()
		for v := range fdbus.SignalListener {
			if fdbus.Conn.Names()[0] == v.Sender {
				fdbus.Log("ignore signal %s generated by socket", v.Name)
			} else {
				fdbus.Log("forwarding signal %s ...", v.Name)
				signal := flowteledbus.CreateFshaperDbusSignal(v)
				if fdbus.ShouldSendSignal(signal) {
					if err := fdbus.Send(signal); err != nil {
						errChannel <- err
						return
					}
				}
			}
		}
	}()

	// don't close the closeChannel to keep running forever
	// fdbus.Close()
	// close(closeChannel)
}
