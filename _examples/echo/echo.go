// Copyright 2018 ETH Zurich
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/netsec-ethz/scion-apps/pkg/pan"
)

func main() {
	var err error
	// get local and remote addresses from program arguments:
	port := flag.Uint("port", 0, "[Server] local port to listen on")
	remoteAddr := flag.String("remote", "", "[Client] Remote (i.e. the server's) SCION Address (e.g. 17-ffaa:1:1,[127.0.0.1]:12345)")
	flag.Parse()

	if (*port > 0) == (len(*remoteAddr) > 0) {
		check(fmt.Errorf("Either specify -port for server or -remote for client"))
	}

	if *port > 0 {
		err = runServer(int(*port))
		check(err)
	} else {
		//for {
		err = runClient(*remoteAddr)
		check(err)
		//}
	}
}

func runServer(port int) error {
	listener, err := pan.ListenUDP(context.Background(), &net.UDPAddr{Port: port}, nil)
	if err != nil {
		fmt.Println("err", err)
		return err
	}
	defer listener.Close()
	fmt.Println(listener.LocalAddr())

	buffer := make([]byte, 16*1024)
	knownConnections := make(map[string]pan.Conn)
	for {
		n, from, err := listener.ReadFrom(buffer)
		if err != nil {
			fmt.Println(err)
			continue
		}
		data := buffer[:n]
		conn := knownConnections[from.String()]
		if conn == nil {
			conn, err = listener.MakeConnectionToRemote(context.Background(), from.(pan.UDPAddr), nil, nil)
			if err != nil {
				fmt.Println(err)
				continue
			}
			knownConnections[from.String()] = conn
			fmt.Printf("Received from new connection %s: %s\n", from, data)
		} else {
			fmt.Printf("Received from known connection %s: %s\n", from, data)
		}

		msg := fmt.Sprintf("take it back! %s", time.Now().Format("15:04:05.0"))
		n, err = conn.Write([]byte(msg))
		fmt.Printf("Wrote %d bytes.\n", n)
		if err != nil {
			fmt.Println(err)
			continue
		}
	}
}

func runClient(address string) error {
	addr, err := pan.ParseUDPAddr(address)
	if err != nil {
		return err
	}
	conn, err := pan.DialUDP(context.Background(), nil, addr, nil, nil)
	if err != nil {
		fmt.Println("err", err)
		return err
	}
	defer conn.Close()
	sent := 0
	got := 0
	go func() {
		buffer := make([]byte, 16*1024)
		for {
			/*n*/ _, err := conn.Read(buffer)
			if err != nil {
				// TODO: There definitely needs to be a better way to check whether a connection is closed than this...
				if strings.Contains(err.Error(), "use of closed network connection") {
					return
				}
				fmt.Println(err)
				continue
			}
			//data := buffer[:n]
			got += 1
			//fmt.Printf("Received reply: %s\n", data)
		}
	}()
	for i := 0; i < 40000; i++ {
		//nBytes, err := conn.Write([]byte(fmt.Sprintf("hello world %s", time.Now().Format("15:04:05.0"))))
		/*nBytes*/
		_, err := conn.Write([]byte(fmt.Sprintf("hello world %s", time.Now().Format("15:04:05.0"))))
		if err != nil {
			return err
		}
		sent += 1
		if (i % 5) == 0 {
			fmt.Printf("Stats: sent %d acks %d lost %d loss %f%%\n", sent, got, sent-got, 100*float64(sent-got)/float64(sent))
		}
		//fmt.Printf("Wrote %d bytes.\n", nBytes)
		time.Sleep(time.Duration(5) * time.Millisecond)
	}
	time.Sleep(time.Duration(10000) * time.Millisecond)
	fmt.Printf("Stats: sent %d acks %d lost %d loss %f%%\n", sent, got, sent-got, 100*float64(sent-got)/float64(sent))
	return nil
}

// Check just ensures the error is nil, or complains and quits
func check(e error) {
	if e != nil {
		fmt.Fprintln(os.Stderr, "Fatal error:", e)
		os.Exit(1)
	}
}
