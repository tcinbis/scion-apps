package utils

import (
	"context"
	"fmt"
	"github.com/lucas-clemente/quic-go"
	"github.com/scionproto/scion/go/lib/addr"
	"github.com/scionproto/scion/go/lib/log"
	sd "github.com/scionproto/scion/go/lib/sciond"
	"github.com/scionproto/scion/go/lib/snet"
	"github.com/scionproto/scion/go/lib/snet/squic"
	"github.com/scionproto/scion/go/lib/sock/reliable"
	"net"
	"os"
)

func SetAddrIA(s string) (t *addr.IA) {
	t = &addr.IA{}
	if s == "" {
		return
	}

	if err := t.Set(s); err != nil {
		log.Debug(fmt.Sprintf("Not setting addrIA to %v\n", s))
		os.Exit(-1)
	}
	return
}

func SetScionPath(s string) (t *ScionPathDescription) {
	t = &ScionPathDescription{}
	if s == "" {
		return
	}

	if err := t.Set(s); err != nil {
		log.Debug(fmt.Sprintf("Not setting scionPath to %v\n", s))
		log.Error(fmt.Sprintf("Setting scionPath: %v\n", err))
		os.Exit(-1)
	}
	return
}

func CheckLocalIA(sciondAddr string, localIA addr.IA) (addr.IA, error) {
	serv := sd.Service{
		Address: sciondAddr,
	}
	sdConn, err := serv.Connect(context.Background())
	if err != nil {
		return localIA, fmt.Errorf("Unable to initialize SCION network: %s", err)
	}
	if localIA.Equal(addr.IA{}) {
		log.Debug("fetchPaths: Got empty localIA. Fetching from SCIOND now...")
		localIA, err = sdConn.LocalIA(context.Background())
		if err != nil {
			return localIA, fmt.Errorf("Error fetching localIA from SCIOND: %v\n", err)
		}
	}
	return localIA, nil
}

func GetSciondService(addr string) sd.Service {
	return sd.Service{
		Address: addr,
	}
}

func GetScionQuicListener(dispatcher string, sciondAddr string, localAddr *net.UDPAddr, localIA addr.IA, quicKeyPath *string, quicPemPath *string, quicConfig *quic.Config) (quic.Listener, error) {
	sciondConn, err := GetSciondService(sciondAddr).Connect(context.Background())
	if err != nil {
		return nil, fmt.Errorf("Unable to initialize SCION network (%s)", err)
	}
	network := snet.NewNetwork(localIA, reliable.NewDispatcher(dispatcher), sd.RevHandler{Connector: sciondConn})
	if *quicKeyPath != "" && *quicPemPath != "" {
		if err = squic.Init(*quicKeyPath, *quicPemPath); err != nil {
			return nil, fmt.Errorf("Unable to load TLS server certificates: %s", err)
		}
	}
	log.Debug(fmt.Sprintf("Listening on %v to with cfg %v\n", localAddr, quicConfig))
	return squic.Listen(network, localAddr, addr.SvcNone, quicConfig)
}

func GetScionQuicSession(dispatcher string, sciondAddr string, localAddr *net.UDPAddr, remoteScionAddr snet.UDPAddr, localIA addr.IA, quicConfig *quic.Config) (quic.Session, error) {
	sciondConn, err := GetSciondService(sciondAddr).Connect(context.Background())
	if err != nil {
		return nil, fmt.Errorf("Unable to initialize SCION network: %s", err)
	}

	network := snet.NewNetwork(localIA, reliable.NewDispatcher(dispatcher), sd.RevHandler{Connector: sciondConn})

	// start QUIC session
	log.Debug(fmt.Sprintf("GetScionQuicSession: %s -- %s\n", localAddr.String(), remoteScionAddr.String()))
	return squic.Dial(network, localAddr, &remoteScionAddr, addr.SvcNone, quicConfig)
}
