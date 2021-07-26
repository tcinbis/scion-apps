// Copyright 2021 ETH Zurich
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
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/lucas-clemente/quic-go/flowtele"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"time"

	"github.com/lucas-clemente/quic-go"
	"github.com/netsec-ethz/scion-apps/pkg/appnet/appquic"
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
		err = runClient(*remoteAddr)
		check(err)
	}
}

func runServer(port int) error {
	tlsCfg := &tls.Config{
		Certificates: appquic.GetDummyTLSCerts(), // XXX
		NextProtos:   []string{"foo"},
	}

	quicCfg := quic.Config{
		FlowTeleSignal: flowtele.CreateFlowteleSignalInterface(nil, nil, nil, nil),
	}

	listener, err := pan.ListenQUIC(context.Background(), &net.UDPAddr{Port: port}, nil, tlsCfg, &quicCfg)
	closerListener := listener.(pan.CloserListener)
	conn := closerListener.Conn.(*pan.UDPListener)

	go func() {
		selector, ok := conn.GetSelector().(*pan.MultiReplySelector)
		if !ok {
			fmt.Println("Error casting selector. Exiting go routine")
			return
		}
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				//selector.ActiveRemotes()

				res, err := json.MarshalIndent(selector, "", "\t")
				check(err)
				f, err := os.Create("output.json")
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

		}
	}()

	if err != nil {
		return err
	}
	defer listener.Close()
	fmt.Println(listener.Addr())

	for {
		session, err := listener.Accept(context.Background())
		session.Context()
		//policy := pan.PolicyFunc(func(paths []*pan.Path) []*pan.Path {
		//	return paths[:3]
		//})
		//remoteAddr, err := pan.ParseUDPAddr(session.RemoteAddr().String())
		//testConn, err := conn.MakeConnectionToRemote(context.Background(), remoteAddr, policy, nil)
		//closerListener.Conn = testConn
		if err != nil {
			return err
		}
		fmt.Println("new session", session.RemoteAddr())
		go func() {
			err := workSession(session)
			if err != nil {
				fmt.Println("error in session", session.RemoteAddr(), err)
			}
		}()
	}
}

func workSession(session quic.Session) error {
	for {
		stream, err := session.AcceptStream(context.Background())
		if err != nil {
			fmt.Println("Error accepting new stream")
			return err
		}
		defer stream.Close()
		data, err := ioutil.ReadAll(stream)
		if err != nil {
			fmt.Println("Error reading from stream")
			return err
		}
		//fmt.Printf("%s\n", data)
		_, err = stream.Write([]byte(fmt.Sprintf("%s gotcha: ", session.RemoteAddr().String())))
		_, err = stream.Write(data)
		if err != nil {
			fmt.Println("Error writing data")
			return err
		}
		stream.Close()
	}
}

func runClient(address string) error {
	addr, err := pan.ParseUDPAddr(address)
	if err != nil {
		return err
	}
	tlsCfg := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"foo"},
	}
	session, err := pan.DialQUIC(context.Background(), nil, addr, nil, nil, "", tlsCfg, nil)
	if err != nil {
		return err
	}
	for {
		stream, err := session.OpenStream()
		if err != nil {
			return err
		}
		_, err = stream.Write([]byte(fmt.Sprintf("hi dude %d", rand.Intn(1000))))
		if err != nil {
			return err
		}
		stream.Close()
		reply, err := ioutil.ReadAll(stream)
		p := session.Conn.GetLastPath()
		if p != nil {
			rp, err := p.Reversed()
			if err != nil {
				fmt.Printf("Error reversing path: %v \n", err)
			}
			if rp.Metadata == nil {
				rp.FetchMetadata()
				p.Metadata = rp.Metadata.Reversed()
			}
			fmt.Printf("Received last packet via: \n %s\n", p.String())
		}
		fmt.Printf("%s\n", reply)
		time.Sleep(10 * time.Millisecond)
	}
}

// Check just ensures the error is nil, or complains and quits
func check(e error) {
	if e != nil {
		fmt.Fprintln(os.Stderr, "Fatal error:", e)
		os.Exit(1)
	}
}
