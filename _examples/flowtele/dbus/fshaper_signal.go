package flowteledbus

import (
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

func CreateFshaperDbusSignal(s *dbus.Signal) DbusSignal {
	elements := strings.Split(s.Name, ".")
	switch elements[len(elements)-1] {
	case "reportRtt":
		return createReportDbusSignalStringIDUint32Uint32(Rtt, s.Body[0].(string), time.Unix(int64(s.Body[1].(uint64)), int64(s.Body[2].(uint32))), s.Body[3].(uint32), 0)
	case "reportLost":
		return createReportDbusSignalStringIDUint32Uint32(Lost, s.Body[0].(string), time.Unix(int64(s.Body[1].(uint64)), int64(s.Body[2].(uint32))), s.Body[3].(uint32), 0)
	case "reportCwnd":
		return createReportDbusSignalStringIDUint32Int32Uint32(Cwnd, s.Body[0].(string), time.Unix(int64(s.Body[1].(uint64)), int64(s.Body[2].(uint32))), s.Body[3].(uint32), s.Body[4].(int32), s.Body[5].(uint32))
	default:
		panic("unimplemented signal")
	}
}

func allFshaperDbusSignals() []introspect.Signal {
	var signals []introspect.Signal
	for _, t := range []QuicDbusSignalType{Rtt, Lost, Cwnd} {
		switch t {
		case Rtt:
			fallthrough
		case Lost:
			signals = append(signals, createReportDbusSignalStringIDUint32Uint32(t, "", time.Now(), 0, 0).IntrospectSignal())
		case Cwnd:
			signals = append(signals, createReportDbusSignalStringIDUint32Int32Uint32(t, "", time.Now(), 0, 0, 0).IntrospectSignal())
		}
	}
	return signals
}
