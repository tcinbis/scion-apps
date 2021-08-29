package flowteledbus

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/lucas-clemente/quic-go/flowtele"
	"github.com/netsec-ethz/scion-apps/_examples/flowtele/dbus/datalogger"
	"github.com/scionproto/scion/go/lib/addr"
)

func GetFlowTeleSignalInterface(loggerCtx context.Context, qdbus *QuicDbus, connID string, localAddr, remoteAddr string, useScion bool, localIA, remoteIA addr.IA, csvPrefix string, waitGroup *sync.WaitGroup) *flowtele.FlowTeleSignal {
	// signal forwarding functions
	srttLogger, lostLogger, cwndLogger := datalogger.CreateDataLoggers(
		loggerCtx,
		useScion,
		csvPrefix,
		waitGroup,
		localIA,
		remoteIA,
		localAddr,
		remoteAddr,
		connID,
	)

	newSrttMeasurement := func(t time.Time, srtt time.Duration) {
		if qdbus.Conn == nil {
			// ignore signals if the session bus is not connected
			return
		}

		if srtt > math.MaxUint32 {
			panic("srtt does not fit in uint32")
		}
		dbusSignal := CreateQuicDbusSignalRtt(connID, t, uint32(srtt.Microseconds()))
		srttLogger.Send(&datalogger.RTTData{FlowID: connID, Timestamp: t, SRtt: srtt})
		if qdbus.ShouldSendSignal(dbusSignal) {
			if err := qdbus.Send(dbusSignal); err != nil {
				fmt.Printf("Error: srtt -> %d\n", qdbus.FlowId)
			}
		}
	}

	packetsLost := func(t time.Time, newSlowStartThreshold uint64) {
		if qdbus.Conn == nil {
			// ignore signals if the session bus is not connected
			return
		}

		if newSlowStartThreshold > math.MaxUint32 {
			panic("newSlotStartThreshold does not fit in uint32")
		}
		dbusSignal := CreateQuicDbusSignalLost(connID, t, uint32(newSlowStartThreshold))
		if qdbus.ShouldSendSignal(dbusSignal) {
			if err := qdbus.Send(dbusSignal); err != nil {
				fmt.Printf("lost -> %d\n", qdbus.FlowId)
			}
		}
	}

	packetsLostRatio := func(t time.Time, lostRatio float64) {
		lostLogger.Send(&datalogger.LostRatioData{FlowID: connID, Timestamp: t, LostRatio: lostRatio})
	}

	packetsAcked := func(t time.Time, congestionWindow uint64, packetsInFlight uint64, ackedBytes uint64) {
		if qdbus.Conn == nil {
			// ignore signals if the session bus is not connected
			return
		}

		if congestionWindow > math.MaxUint32 {
			panic("congestionWindow does not fit in uint32")
		}
		if packetsInFlight > math.MaxInt32 {
			panic("packetsInFlight does not fit in int32")
		}
		if ackedBytes > math.MaxUint32 {
			panic("ackedBytes does not fit in uint32")
		}
		ackedBytesSum := qdbus.Acked(uint32(ackedBytes))
		cwndLogger.Send(&datalogger.CwndData{FlowID: connID, Timestamp: t, Cwnd: congestionWindow})
		dbusSignal := CreateQuicDbusSignalCwnd(connID, t, uint32(congestionWindow), int32(packetsInFlight), ackedBytesSum)
		if qdbus.ShouldSendSignal(dbusSignal) {
			if err := qdbus.Send(dbusSignal); err != nil {
				fmt.Printf("ack -> %d\n", qdbus.FlowId)
			}
			qdbus.ResetAcked()
		}
	}

	return flowtele.CreateFlowteleSignalInterface(newSrttMeasurement, packetsLost, packetsLostRatio, packetsAcked)
}
