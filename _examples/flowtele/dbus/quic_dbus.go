package flowteledbus

import (
	"context"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/lucas-clemente/quic-go"
)

const (
	QUIC_SERVICE_NAME   = "ch.ethz.netsec.flowtele.quic"
	QUIC_INTERFACE_NAME = "ch.ethz.netsec.flowtele.quic"
	QUIC_OBJECT_PATH    = "/ch/ethz/netsec/flowtele/quic"

	LOG_INTERVAL = time.Millisecond * 5
)

type quicDbusMethodInterface struct {
	quicDbus *QuicDbus
}

func (qdbmi quicDbusMethodInterface) ApplyControl(dType uint32, beta float64, cwnd_adjust int64, cwnd_max_adjust int64, use_conservative_allocation bool) (bool, *dbus.Error) {
	start := time.Now()
	qdb := qdbmi.quicDbus
	session := qdb.Session
	if !qdb.applyControl {
		qdb.Log("not forwarding ApplyControl to QUIC flow %d", qdb.FlowId)
		return false, nil
	} else if session != nil {
		ret := session.ApplyControl(beta, cwnd_adjust, cwnd_max_adjust, use_conservative_allocation)
		qdb.Log("apply control returned %t at %v", ret, time.Now().Sub(start))
		return ret, nil
	} else {
		qdb.Log("QUIC session not set, received ApplyControl(%d, %f, %d, %d, %t)", dType, beta, cwnd_adjust, cwnd_max_adjust, use_conservative_allocation)
		return false, nil
	}
}

type QuicDbus struct {
	DbusBase
	FlowId             int32
	Session            quic.FlowTeleSession
	lastLogTime        map[QuicDbusSignalType]time.Time
	logMessagesSkipped map[QuicDbusSignalType]uint64
	applyControl       bool

	// peer identifies a quic session communicating with another machine which is identified by this string. Can be an IP/port or SCION address
	peer string
}

func NewQuicDbus(flowId int32, applyControl bool, peer string) *QuicDbus {
	var d QuicDbus
	d.Init()
	d.FlowId = flowId
	d.peer = peer
	d.applyControl = applyControl
	d.ServiceName = getQuicServiceName(flowId, peer)
	d.ObjectPath = getQuicObjectPath(flowId, peer)
	d.InterfaceName = getQuicInterfaceName(flowId, peer)
	d.LogPrefix = fmt.Sprintf("QUIC_%d", d.FlowId)
	d.ExportedMethods = quicDbusMethodInterface{quicDbus: &d}
	d.SignalMatchOptions = []dbus.MatchOption{}
	d.ExportedSignals = allQuicDbusSignals()
	d.LogSignals = true
	d.SetLogMinIntervalForAllSignals(time.Second)
	return &d
}

func NewQuicDbusCtx(ctx context.Context, flowId int32, applyControl bool, peer string) *QuicDbus {
	var d QuicDbus
	d.Init()
	d.context = ctx
	d.FlowId = flowId
	d.peer = peer
	d.applyControl = applyControl
	d.ServiceName = getQuicServiceName(flowId, peer)
	d.ObjectPath = getQuicObjectPath(flowId, peer)
	d.InterfaceName = getQuicInterfaceName(flowId, peer)
	d.LogPrefix = fmt.Sprintf("QUIC_%d", d.FlowId)
	d.ExportedMethods = quicDbusMethodInterface{quicDbus: &d}
	d.SignalMatchOptions = []dbus.MatchOption{}
	d.ExportedSignals = allQuicDbusSignals()
	d.LogSignals = true
	d.SetLogMinIntervalForAllSignals(time.Second)

	if d.context != nil {
		go d.observeContext()
	}

	return &d
}

func (qdb *QuicDbus) Reinit(flowId int32, applyControl bool, peer string) {
	qdb.FlowId = flowId
	qdb.peer = peer
	qdb.applyControl = applyControl
	qdb.ServiceName = getQuicServiceName(flowId, peer)
	qdb.ObjectPath = getQuicObjectPath(flowId, peer)
	qdb.InterfaceName = getQuicInterfaceName(flowId, peer)
	qdb.LogPrefix = fmt.Sprintf("QUIC_%s", peer)
}

func (qdb *QuicDbus) LogRtt(t time.Time, rtt time.Duration) {
	val, ok := qdb.lastLogTime[Rtt]
	if !ok || t.Sub(val) > LOG_INTERVAL {
		nSkipped := uint64(0)
		if val2, ok2 := qdb.logMessagesSkipped[Rtt]; ok2 {
			nSkipped = val2
		}
		qdb.Log("RTT (skipped %d) srtt = %d", nSkipped, rtt.Milliseconds())
		qdb.lastLogTime[Rtt] = t
		qdb.logMessagesSkipped[Rtt] = 0
	} else {
		qdb.logMessagesSkipped[Rtt] = qdb.logMessagesSkipped[Rtt] + 1
	}
}

func (qdb *QuicDbus) LogLost(t time.Time, ssthresh uint64) {
	val, ok := qdb.lastLogTime[Lost]
	if !ok || t.Sub(val) > LOG_INTERVAL {
		nSkipped := uint64(0)
		if val2, ok2 := qdb.logMessagesSkipped[Lost]; ok2 {
			nSkipped = val2
		}
		qdb.Log("Lost (skipped %d) ssthresh = %d", nSkipped, ssthresh)
		qdb.lastLogTime[Lost] = t
		qdb.logMessagesSkipped[Lost] = 0
	} else {
		qdb.logMessagesSkipped[Lost] = qdb.logMessagesSkipped[Lost] + 1
	}
}

func (qdb *QuicDbus) LogAcked(t time.Time, congestionWindow uint64, packetsInFlight uint64, ackedBytes uint64) {
	val, ok := qdb.lastLogTime[Cwnd]
	if !ok || t.Sub(val) > LOG_INTERVAL {
		nSkipped := uint64(0)
		if val2, ok2 := qdb.logMessagesSkipped[Cwnd]; ok2 {
			nSkipped = val2
		}
		qdb.Log("Cwnd (skipped %d) cwnd = %d, inflight = %d, acked = %d", nSkipped, congestionWindow, packetsInFlight, ackedBytes)
		qdb.lastLogTime[Cwnd] = t
		qdb.logMessagesSkipped[Cwnd] = 0
	} else {
		qdb.logMessagesSkipped[Cwnd] = qdb.logMessagesSkipped[Cwnd] + 1
	}
}

func (qdb *QuicDbus) SendRttSignal(t time.Time, rtt uint32) error {
	return qdb.Send(CreateQuicDbusSignalRtt(qdb.peer, t, rtt))
}

func (qdb *QuicDbus) SendLostSignal(t time.Time, newSsthresh uint32) error {
	return qdb.Send(CreateQuicDbusSignalLost(qdb.peer, t, newSsthresh))
}

func (qdb *QuicDbus) SendLostRatioSignal(t time.Time, ratio float64) error {
	return qdb.Send(CreateQuicDbusSignalLostRatio(qdb.peer, t, ratio))
}

func (qdb *QuicDbus) SendCwndSignal(t time.Time, cwnd uint32, pktsInFlight int32, ackedBytes uint32) error {
	return qdb.Send(CreateQuicDbusSignalCwnd(qdb.peer, t, cwnd, pktsInFlight, ackedBytes))
}

func getQuicServiceName(flowId int32, peer string) string {
	return fmt.Sprintf("%s_%s", QUIC_SERVICE_NAME, peer)
}

func getQuicObjectPath(flowId int32, peer string) dbus.ObjectPath {
	return dbus.ObjectPath(fmt.Sprintf("%s_%s", QUIC_OBJECT_PATH, peer))
}

func getQuicInterfaceName(flowId int32, peer string) string {
	return fmt.Sprintf("%s_%s", QUIC_INTERFACE_NAME, peer)
}
