package flowteledbus

import (
	"time"

	"github.com/godbus/dbus/v5/introspect"
)

func CreateQuicDbusSignalRtt(flow string, t time.Time, srtt_us uint32) DbusSignal {
	return createReportDbusSignalStringIDUint32(Rtt, flow, t, srtt_us)
}

func CreateQuicDbusSignalLost(flow string, t time.Time, newSsthresh uint64) DbusSignal {
	return createReportDbusSignalStringIDUint64(Lost, flow, t, newSsthresh)
}

func CreateQuicDbusSignalLostRatio(flow string, t time.Time, ratio float64) DbusSignal {
	return createReportDbusSignalStringIDFloat64(LostRatio, flow, t, ratio)
}

func CreateQuicDbusSignalCwnd(flow string, t time.Time, cwnd uint32, pktsInFlight int32, ackedBytes uint32) DbusSignal {
	return createReportDbusSignalStringIDUint32Int32Uint32(Cwnd, flow, t, cwnd, pktsInFlight, ackedBytes)
}

func allQuicDbusSignals() []introspect.Signal {
	return []introspect.Signal{
		CreateQuicDbusSignalRtt("deadbeef", time.Now(), 0).IntrospectSignal(),
		CreateQuicDbusSignalLost("deadbeef", time.Now(), 0).IntrospectSignal(),
		CreateQuicDbusSignalCwnd("deadbeef", time.Now(), 0, 0, 0).IntrospectSignal(),
	}
}
