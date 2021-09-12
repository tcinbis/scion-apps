package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lucas-clemente/quic-go"
	"github.com/netsec-ethz/scion-apps/pkg/appnet/appquic"
	"github.com/netsec-ethz/scion-apps/pkg/pan"
	"github.com/scionproto/scion/go/lib/log"

	"github.com/scionproto/scion/go/lib/serrors"
	"github.com/scionproto/scion/go/lib/snet"

	"github.com/netsec-ethz/scion-apps/pkg/appnet"
)

const (
	BIT     = 1
	KILOBIT = 1000 * BIT
	MEGABIT = 1000 * KILOBIT

	BYTE     = 8 * BIT
	KILOBYTE = 1000 * BYTE
	MEGABYTE = 1000 * KILOBYTE
)

var (
	port        = flag.Uint("port", 0, "[Server] Local port to listen on")
	local       = flag.String("local", "", "[Server] If specified defines the local address to listen on.")
	payload     = flag.Uint("payload", 10000000, "[Client] Size of each burst in bytes")
	interactive = flag.Bool("interactive", false, "[Client] Select the path interactively")
	usePan      = flag.Bool("pan", false, "[Server] Whether to use PAN instead of appquic.")
	remoteAddr  = flag.String("remote", "", "[Client] Remote (i.e. the server's) SCION Address (e.g. 17-ffaa:1:1,[127.0.0.1]:12345)")
	scion       = flag.Bool("scion", false, "[Server/Client] Enables the use of SCION.")
)

func main() {
	flag.Parse()

	if (*port > 0) == (len(*remoteAddr) > 0) {
		fmt.Println("Either specify -port for server or -remote for client")
		os.Exit(1)
	}

	var err error
	if *port > 0 {
		err = runServer(*local, int(*port), int(*payload), *usePan)
	} else {
		err = runClient(*remoteAddr, int(*payload), *interactive)
	}
	if err != nil {
		fmt.Println("err", err)
		os.Exit(1)
	}
}

func send(stream quic.Stream, payloadSize int) {
	buffer := make([]byte, payloadSize)
	stream.Write([]byte{0})
	_, err := io.ReadFull(stream, make([]byte, 1))
	if err != nil {
		return
	}

	for {
		fmt.Printf("Sending %.2f MByte\n", float64(payloadSize*BYTE)/MEGABYTE)
		_, err := stream.Write(buffer)
		if err != nil {
			fmt.Println(err)
			break
		}
	}
}

func receive(ctx context.Context, stream quic.Stream, payloadSize int) {
	stream.Write([]byte{0})
	_, err := io.ReadFull(stream, make([]byte, 1))
	if err != nil {
		return
	}

	fmt.Println("Stream opened. Ready for receiving.")
	avg := make([]float64, 0, 1000)
	buffer := make([]byte, payloadSize)
	receivedCount := 0
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		default:
			tStart := time.Now()
			count, err := io.ReadFull(stream, buffer)
			tEnd := time.Now()
			if err != nil {
				fmt.Println(err)
				break loop
			}
			receivedCount += count
			tCur := tEnd.Sub(tStart).Seconds()
			curRate := float64(count*BYTE) / tCur / MEGABIT
			avg = append(avg, curRate)
			fmt.Printf("cur: %.1fMBit/s %.2fs\n", curRate, tCur)
		}
	}

	avgSum := 0.0
	for _, v := range avg {
		avgSum += v
	}
	fmt.Printf("Average througput: %.2f Mbit/s\n", avgSum/float64(len(avg)))
}

func runClient(address string, payloadSize int, interactive bool) error {
	var sess quic.Session
	var path snet.Path
	if *scion {
		addr, err := appnet.ResolveUDPAddr(address)
		if err != nil {
			return err
		}
		if interactive {
			path, err = appnet.ChoosePathInteractive(addr.IA)
			if err != nil {
				return err
			}
			appnet.SetPath(addr, path)
		} else {
			paths, err := appnet.QueryPaths(addr.IA)
			if err != nil {
				return err
			}
			path = paths[0]
		}
		sess, err = appquic.DialAddr(addr, "", &tls.Config{NextProtos: []string{"speed"}, InsecureSkipVerify: true}, nil)
		if err != nil {
			return err
		}
	} else {
		var err error
		sess, err = quic.DialAddr(address, &tls.Config{NextProtos: []string{"speed"}, InsecureSkipVerify: true}, nil)
		if err != nil {
			return err
		}
	}
	defer sess.CloseWithError(quic.ApplicationErrorCode(quic.NoError), "NoError")

	fmt.Printf("Running client using payload size %v", payloadSize)
	if *scion {
		fmt.Printf("via %v", path)
	}
	fmt.Printf("\n")

	stream, err := sess.OpenStreamSync(context.Background())
	if err != nil {
		return fmt.Errorf("Error opening QUIC stream: %s", err)
	}
	defer func() {
		fmt.Println("closing stream")
		if err := stream.Close(); err != nil {
			fmt.Printf("Deffered Error closing stream: %v\n", err)
		}
	}()

	send(stream, payloadSize)
	//receive(stream, payloadSize)
	return nil
}

func runServer(localAddr string, port, payloadSize int, usePan bool) error {
	sigs := make(chan os.Signal, 1)
	ctx, cancel := context.WithCancel(context.Background())
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		defer log.HandlePanic()
		sig := <-sigs
		fmt.Printf("%v\n", sig)
		cancel()
	}()
	defer cancel()

	var listener quic.Listener
	var err error
	if *scion {
		if usePan {
			listener, err = pan.ListenQUIC(ctx, &net.UDPAddr{Port: port}, nil, &tls.Config{Certificates: appquic.GetDummyTLSCerts(), NextProtos: []string{"speed"}, InsecureSkipVerify: true}, nil)
			fmt.Printf("%v\n", listener.Addr())
		} else if localAddr != "" {
			listener, err = appquic.Listen(&net.UDPAddr{
				IP:   net.ParseIP(localAddr),
				Port: port,
			}, &tls.Config{Certificates: appquic.GetDummyTLSCerts(), NextProtos: []string{"speed"}, InsecureSkipVerify: true}, nil)
			fmt.Printf("%v,%v\n", appnet.DefNetwork().IA, listener.Addr())
		} else {
			listener, err = appquic.ListenPort(uint16(port), &tls.Config{Certificates: appquic.GetDummyTLSCerts(), NextProtos: []string{"speed"}, InsecureSkipVerify: true}, nil)
			fmt.Printf("%v,%v\n", appnet.DefNetwork().IA, listener.Addr())
		}
	} else {
		listener, err = quic.ListenAddr(fmt.Sprintf("%s:%d", localAddr, port), &tls.Config{Certificates: appquic.GetDummyTLSCerts(), NextProtos: []string{"speed"}, InsecureSkipVerify: true}, nil)
		fmt.Printf("%v\n", listener.Addr())
	}

	if err != nil {
		return serrors.WrapStr("can't listen:", err)
	}

	defer listener.Close()

	sess, err := listener.Accept(ctx)
	if err != nil {
		return fmt.Errorf("Error accepting sessions: %s\n", err)
	}
	stream, err := sess.AcceptStream(ctx)
	if err != nil {
		return fmt.Errorf("Error accepting streams: %s\n", err)
	}

	receive(ctx, stream, payloadSize)
	return nil
}
