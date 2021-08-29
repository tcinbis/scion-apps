package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/netsec-ethz/scion-apps/_examples/flowtele/utils"
	"github.com/netsec-ethz/scion-apps/pkg/appnet"
	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/lucas-clemente/quic-go"

	"github.com/scionproto/scion/go/lib/addr"
	"github.com/scionproto/scion/go/lib/log"
	sd "github.com/scionproto/scion/go/lib/sciond"
	"github.com/scionproto/scion/go/lib/snet"
)

const (
	errorNoError quic.ApplicationErrorCode = 0x100
)

var (
	tlsConfig       tls.Config
	localIAFromFlag addr.IA

	listenAddr   = kingpin.Flag("ip", "IP address to listen on").Default("127.0.0.1").String()
	listenPort   = kingpin.Flag("port", "Port number to listen on").Default("5500").Int()
	nConnections = kingpin.Flag("num", "Number of QUIC connections allowed per port number").Default("12").Int()
	portRange    = kingpin.Flag("port-range", "Number of ports (increasing from --port) that are accepting QUIC connections").Default("1").Int()
	keyPath      = kingpin.Flag("key", "TLS key file").Default("go/flowtele/tls.key").String()
	pemPath      = kingpin.Flag("pem", "TLS certificate file").Default("go/flowtele/tls.pem").String()
	messageSize  = kingpin.Flag("message-size", "size of the message that should be received as a whole").Default("10000000").Int()

	useScion       = kingpin.Flag("scion", "Open scion quic sockets").Default("false").Bool()
	dispatcherFlag = kingpin.Flag("dispatcher", "Path to dispatcher socket").Default("").String()
	sciondAddrFlag = kingpin.Flag("sciond", "SCIOND address").Default(sd.DefaultAPIAddress).String()
	localIAFlag    = kingpin.Flag("local-ia", "ISD-AS address to listen on.").String()
)

var (
	sigs = make(chan os.Signal, 1)
	done = make(chan struct{}, 1)
)

func init() {
	utils.SetupLogger()
	kingpin.Parse()
	localIAFromFlag = *utils.SetAddrIA(*localIAFlag)
}

// create certificate and key with
// openssl req -new -newkey rsa:4096 -x509 -sha256 -days 365 -nodes -out tls.pem -keyout tls.key
func initTlsCert() error {
	cert, err := tls.LoadX509KeyPair(*pemPath, *keyPath)
	if err != nil {
		log.Error(fmt.Sprintf("Unable to load TLS cert (%s) or key (%s): %s\n", *pemPath, *keyPath, err))
		return err
	}
	tlsConfig.Certificates = []tls.Certificate{cert}
	tlsConfig.NextProtos = []string{"Flowtele"}
	return nil
}

func getQuicListener(lAddr *net.UDPAddr) (quic.Listener, error) {
	quicConfig := &quic.Config{MaxIdleTimeout: time.Hour}
	if *useScion {
		localIA, err := utils.CheckLocalIA(*sciondAddrFlag, localIAFromFlag)
		if err != nil {
			log.Error("Error receiving localIA from SCIOND.")
		}
		return utils.GetScionQuicListener(*dispatcherFlag, *sciondAddrFlag, lAddr, localIA, keyPath, pemPath, quicConfig)
	} else {
		conn, err := net.ListenUDP("udp", lAddr)
		if err != nil {
			log.Error(fmt.Sprintf("Error starting UDP listener: %s\n", err))
			return nil, err
		}
		initTlsCert()
		// make QUIC idle timout long to allow a delay between starting the listeners and the senders
		return quic.Listen(conn, &tlsConfig, quicConfig)
	}
}

func acceptStream(listener quic.Listener, ctx context.Context) (quic.Session, quic.Stream, error) {
	session, err := listener.Accept(ctx)
	if err != nil {
		log.Error(fmt.Sprintf("Error accepting sessions: %s\n", err))
		return nil, nil, err
	} else {
		log.Debug("Accepted session")
	}
	stream, err := session.AcceptStream(ctx)
	if err != nil {
		log.Error(fmt.Sprintf("Error accepting streams: %s\n", err))
		return nil, nil, err
	} else {
		log.Debug(fmt.Sprintf("Accepted stream %d\n", stream.StreamID()))
	}
	return session, stream, nil
}

func getPort(addr net.Addr) (int, error) {
	switch addr.(type) {
	case *net.UDPAddr:
		return addr.(*net.UDPAddr).Port, nil
	case *snet.UDPAddr:
		return addr.(*snet.UDPAddr).Host.Port, nil
	default:
		return 0, fmt.Errorf("Unknown address type")
	}
}

func listenOnStream(session quic.Session, stream quic.Stream) error {
	defer func() {
		log.HandlePanic()
		log.Debug(fmt.Sprintf("Closing stream: %d\n", stream.StreamID()))
		if err := stream.Close(); err != nil {
			log.Error(fmt.Sprintf("Error closing stream: %v\n", err))
		}
	}()
	defer func() {
		log.HandlePanic()
		log.Debug(fmt.Sprintf("Closing session: %s\n", session.RemoteAddr().String()))
		if err := session.CloseWithError(errorNoError, ""); err != nil {
			log.Error(fmt.Sprintf("Error closing session: %v\n", err))
		}
	}()

	message := make([]byte, *messageSize)
	tInit := time.Now()
	nTot := 0
	rPort, err := getPort(session.RemoteAddr())
	if err != nil {
		log.Error(fmt.Sprintf("Error resolving remote UDP address: %s\n", err))
		return err
	}
	lPort, err := getPort(session.LocalAddr())
	if err != nil {
		log.Error(fmt.Sprintf("Error resolving local UDP address: %s\n", err))
		return err
	}
	log.Info(fmt.Sprintf("%d_%d: Listening on Stream %d\n", lPort, rPort, stream.StreamID()))

loop:
	for {
		select {
		case <-done:
			break loop
		default:
			tStart := time.Now()
			n, err := io.ReadFull(stream, message)
			if err != nil {
				if err == io.ErrUnexpectedEOF {
					// sender stopped sending
					return nil
				} else {
					return fmt.Errorf("Error reading message: %s\n", err)
				}
			}
			tEnd := time.Now()
			nTot += n
			tCur := tEnd.Sub(tStart).Seconds()
			tTot := tEnd.Sub(tInit).Seconds()
			// Mbit/s
			curRate := float64(n) / tCur / 1000000.0 * 8.0
			totRate := float64(nTot) / tTot / 1000000.0 * 8.0
			log.Info(fmt.Sprintf("%d_%d cur: %.1fMbit/s or %.1f MByte/s (%.1fMB in %.2fs), tot: %.1fMbit/s (%.1fMB in %.2fs)\n", lPort, rPort, curRate, curRate/8, float64(n)/1000000, tCur, totRate, float64(nTot)/1000000, tTot))
		}
	}
	return nil
}

func main() {
	errs := make(chan error)
	utils.SetupLogger()

	// capture interrupts to gracefully terminate run
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		defer log.HandlePanic()
		sig := <-sigs
		log.Debug(fmt.Sprintf("Received: %v\n", sig))
		close(done)
	}()

	var wg sync.WaitGroup
	wg.Add(*portRange * *nConnections)
	for i := 0; i < *portRange; i++ {
		go func(port int) {
			defer log.HandlePanic()
			listener, err := getQuicListener(&net.UDPAddr{IP: net.ParseIP(*listenAddr), Port: port})
			if err != nil {
				errs <- fmt.Errorf("Error starting QUIC listener: %s", err)
				return
			}
			log.Info(fmt.Sprintf("%v,%v\n", appnet.DefNetwork().IA, listener.Addr()))

			// defer listener.Close()
			log.Info(fmt.Sprintf("Listening for QUIC connections on %s\n", listener.Addr().String()))
			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				defer log.HandlePanic()
			loop:
				for {
					select {
					case <-done:
						cancel()
						break loop
					}
				}
			}()
			for j := 0; j < *nConnections; j++ {
				session, stream, err := acceptStream(listener, ctx)
				if err != nil {
					errs <- err
					return
				}
				go func(se quic.Session, st quic.Stream) {
					defer log.HandlePanic()
					defer wg.Done()

					go func() {
						defer log.HandlePanic()
						for range done {
							if err := stream.Close(); err != nil {
								log.Error(fmt.Sprintf("Error closing stream: %v\n", err))
							}
							log.Debug(fmt.Sprintf("Closed stream.\n"))
							if err := session.CloseWithError(errorNoError, "Interrupt received."); err != nil {
								log.Error(fmt.Sprintf("Error closing session: %v\n", err))
							}
							log.Debug("Closed session.")
						}
					}()

					if err := listenOnStream(se, st); err != nil {
						errs <- err
					}
					log.Debug(fmt.Sprintf("Function listenOnStream for %v returned.\n", se.RemoteAddr().String()))
				}(session, stream)
			}
		}(*listenPort + i)
	}
	closeChannel := make(chan struct{})
	go func() {
		defer log.HandlePanic()
		wg.Wait()
		close(closeChannel)
	}()
	select {
	case err := <-errs:
		log.Error(fmt.Sprintf("Error encountered (%s), stopping all listeners\n", err))
		return
	case <-closeChannel:
		log.Info("Exiting without errors")
	}
}
