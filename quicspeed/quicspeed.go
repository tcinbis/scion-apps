package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"github.com/lucas-clemente/quic-go"
	"github.com/netsec-ethz/scion-apps/pkg/appnet/appquic"
	"github.com/netsec-ethz/scion-apps/pkg/pan"
	"io"
	"net"
	"os"
	"time"

	"github.com/scionproto/scion/go/lib/serrors"
	"github.com/scionproto/scion/go/lib/snet"

	"github.com/netsec-ethz/scion-apps/pkg/appnet"
)

const (
	BIT      = 1
	BYTE     = 8 * BIT
	KILOBYTE = 1000 * BYTE
	MEGABYTE = 1000 * KILOBYTE
)

func main() {
	port := flag.Uint("port", 0, "[Server] Local port to listen on")
	payload := flag.Uint("payload", 10000000, "[Client] Size of each burst in bytes")
	interactive := flag.Bool("interactive", false, "[Client] Select the path interactively")
	usePan := flag.Bool("pan", false, "[Server] Whether to use PAN instead of appquic.")
	remoteAddr := flag.String("remote", "", "[Client] Remote (i.e. the server's) SCION Address (e.g. 17-ffaa:1:1,[127.0.0.1]:12345)")
	flag.Parse()

	if (*port > 0) == (len(*remoteAddr) > 0) {
		fmt.Println("Either specify -port for server or -remote for client")
		os.Exit(1)
	}

	var err error
	if *port > 0 {
		err = runServer(int(*port), int(*payload), *usePan)
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
		}
	}
}

func receive(stream quic.Stream, payloadSize int) {
	stream.Write([]byte{0})
	_, err := io.ReadFull(stream, make([]byte, 1))
	if err != nil {
		return
	}

	fmt.Println("Stream opened. Ready for receiving.")
	buffer := make([]byte, payloadSize)
	receivedCount := 0
	for {
		tStart := time.Now()
		count, err := io.ReadFull(stream, buffer)
		tEnd := time.Now()
		if err != nil {
			fmt.Println(err)
			continue
		}
		receivedCount += count
		tCur := tEnd.Sub(tStart).Seconds()
		curRate := float64(count*BYTE) / tCur / MEGABYTE

		fmt.Printf("cur: %.1fMByte/s %.2fs\n", curRate, tCur)
	}
}

func runClient(address string, payloadSize int, interactive bool) error {
	addr, err := appnet.ResolveUDPAddr(address)
	if err != nil {
		return err
	}
	var path snet.Path
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

	sess, err := appquic.Dial(address, &tls.Config{NextProtos: []string{"speed"}, InsecureSkipVerify: true}, nil)
	if err != nil {
		return err
	}
	defer sess.CloseWithError(quic.ApplicationErrorCode(quic.NoError), "NoError")

	fmt.Printf("Running client using payload size %v via %v\n", payloadSize, path)

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

	//send(stream, payloadSize)
	receive(stream, payloadSize)
	return nil
}

func runServer(port, payloadSize int, usePan bool) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var listener quic.Listener
	var err error
	if usePan {
		listener, err = pan.ListenQUIC(ctx, &net.UDPAddr{Port: port}, nil, &tls.Config{Certificates: appquic.GetDummyTLSCerts(), NextProtos: []string{"speed"}, InsecureSkipVerify: true}, nil)
		fmt.Printf("%v\n", listener.Addr())
	} else {
		listener, err = appquic.ListenPort(uint16(port), &tls.Config{Certificates: appquic.GetDummyTLSCerts(), NextProtos: []string{"speed"}, InsecureSkipVerify: true}, nil)
		fmt.Printf("%v,%v\n", appnet.DefNetwork().IA, listener.Addr())
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

	send(stream, payloadSize)
	//receive(stream, payloadSize)
	return nil
}
