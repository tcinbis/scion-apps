package flowteledbus

import (
	"reflect"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

type DbusSignal interface {
	Name() string
	Values() []interface{}
	IntrospectSignal() introspect.Signal
	SignalType() QuicDbusSignalType
}

type QuicDbusSignalType int

const (
	Rtt QuicDbusSignalType = iota // = 0
	Lost
	Cwnd
	Pacing
	BbrRtt
	BbrBW
	Delivered
	DeliveredAdjust
	GainLost
	LostRatio
)

type dbusSignalStruct struct {
	Type  QuicDbusSignalType
	Value interface{}
}

func (s *dbusSignalStruct) Name() string {
	switch s.Type {
	case Rtt:
		return "reportRtt"
	case Lost:
		return "reportLost"
	case Cwnd:
		return "reportCwnd"
	case Pacing:
		return "reportPacing"
	case BbrRtt:
		return "reportBbrRtt"
	case BbrBW:
		return "reportBbrBW"
	case Delivered:
		return "reportDelivered"
	case DeliveredAdjust:
		return "reportDeliveredAdjust"
	case GainLost:
		return "reportGainLost"
	default:
		panic("invalid signal type")
	}
}

func (s *dbusSignalStruct) Values() []interface{} {
	return transform_struct_to_field_list(s.Value)
}

func (s *dbusSignalStruct) IntrospectSignal() introspect.Signal {
	var arg_list []introspect.Arg
	t := reflect.TypeOf(s.Value)
	for i := 0; i < t.NumField(); i++ {
		arg_list = append(arg_list, introspect.Arg{Name: t.Field(i).Name, Type: dbus.SignatureOfType(t.Field(i).Type).String(), Direction: "out"})
	}
	return introspect.Signal{Name: s.Name(), Args: arg_list}
}

func (s *dbusSignalStruct) SignalType() QuicDbusSignalType {
	return s.Type
}

func createReportDbusSignalUint32(t QuicDbusSignalType, flow int32, time time.Time, v0 uint32) DbusSignal {
	return &dbusSignalStruct{t, struct {
		Flow   int32
		TvSec  uint64
		TvNsec uint32
		V0     uint32
	}{
		int32(flow),
		uint64(time.Unix()),
		uint32(time.Nanosecond()),
		v0,
	}}
}

func createReportDbusSignalUint32Uint32(t QuicDbusSignalType, flow int32, time time.Time, v0 uint32, v1 uint32) DbusSignal {
	return &dbusSignalStruct{t, struct {
		Flow   int32
		TvSec  uint64
		TvNsec uint32
		V0     uint32
		V1     uint32
	}{
		int32(flow),
		uint64(time.Unix()),
		uint32(time.Nanosecond()),
		v0,
		v1,
	}}
}

func createReportDbusSignalStringIDUint32Uint32(t QuicDbusSignalType, flow string, time time.Time, v0 uint32, v1 uint32) DbusSignal {
	return &dbusSignalStruct{t, struct {
		Flow   string
		TvSec  uint64
		TvNsec uint32
		V0     uint32
		V1     uint32
	}{
		string(flow),
		uint64(time.Unix()),
		uint32(time.Nanosecond()),
		v0,
		v1,
	}}
}

func createReportDbusSignalStringIDUint64Uint32(t QuicDbusSignalType, flow string, time time.Time, v0 uint64, v1 uint32) DbusSignal {
	return &dbusSignalStruct{t, struct {
		Flow   string
		TvSec  uint64
		TvNsec uint32
		V0     uint64
		V1     uint32
	}{
		string(flow),
		uint64(time.Unix()),
		uint32(time.Nanosecond()),
		v0,
		v1,
	}}
}

func createReportDbusSignalUint32Int32(t QuicDbusSignalType, flow int32, time time.Time, v0 uint32, v1 int32) DbusSignal {
	return &dbusSignalStruct{t, struct {
		Flow   int32
		TvSec  uint64
		TvNsec uint32
		V0     uint32
		V1     int32
	}{
		int32(flow),
		uint64(time.Unix()),
		uint32(time.Nanosecond()),
		v0,
		v1,
	}}
}

func createReportDbusSignalUint32Int32Uint32(t QuicDbusSignalType, flow int32, time time.Time, v0 uint32, v1 int32, v2 uint32) DbusSignal {
	return &dbusSignalStruct{t, struct {
		Flow   int32
		TvSec  uint64
		TvNsec uint32
		V0     uint32
		V1     int32
		V2     uint32
	}{
		int32(flow),
		uint64(time.Unix()),
		uint32(time.Nanosecond()),
		v0,
		v1,
		v2,
	}}
}

func createReportDbusSignalStringIDUint32Int32Uint32(t QuicDbusSignalType, flow string, time time.Time, v0 uint32, v1 int32, v2 uint32) DbusSignal {
	return &dbusSignalStruct{t, struct {
		Flow   string
		TvSec  uint64
		TvNsec uint32
		V0     uint32
		V1     int32
		V2     uint32
	}{
		string(flow),
		uint64(time.Unix()),
		uint32(time.Nanosecond()),
		v0,
		v1,
		v2,
	}}
}

func createReportDbusSignalFloat64(t QuicDbusSignalType, flow int32, time time.Time, v0 float64) DbusSignal {
	return &dbusSignalStruct{t, struct {
		Flow   int32
		TvSec  uint64
		TvNsec uint32
		V0     float64
	}{
		int32(flow),
		uint64(time.Unix()),
		uint32(time.Nanosecond()),
		v0,
	}}
}

func createReportDbusSignalStringIDFloat64(t QuicDbusSignalType, flow string, time time.Time, v0 float64) DbusSignal {
	return &dbusSignalStruct{t, struct {
		Flow   string
		TvSec  uint64
		TvNsec uint32
		V0     float64
	}{
		string(flow),
		uint64(time.Unix()),
		uint32(time.Nanosecond()),
		v0,
	}}
}

func createReportDbusSignalStringIDUint32(t QuicDbusSignalType, flow string, time time.Time, v0 uint32) DbusSignal {
	return &dbusSignalStruct{t, struct {
		Flow   string
		TvSec  uint64
		TvNsec uint32
		V0     uint32
	}{
		string(flow),
		uint64(time.Unix()),
		uint32(time.Nanosecond()),
		v0,
	}}
}

func createReportDbusSignalStringIDUint64(t QuicDbusSignalType, flow string, time time.Time, v0 uint64) DbusSignal {
	return &dbusSignalStruct{t, struct {
		Flow   string
		TvSec  uint64
		TvNsec uint32
		V0     uint64
	}{
		string(flow),
		uint64(time.Unix()),
		uint32(time.Nanosecond()),
		v0,
	}}
}

func transform_struct_to_field_list(in_struct interface{}) []interface{} {
	var field_list []interface{}
	v := reflect.ValueOf(in_struct)
	t := reflect.TypeOf(in_struct)
	for i := 0; i < t.NumField(); i++ {
		field_list = append(field_list, v.Field(i).Interface())
	}
	return field_list
}
