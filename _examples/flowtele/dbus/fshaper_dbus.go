package flowteledbus

import (
	"fmt"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	SERVICE_NAME   = "ch.ethz.netsec.flowtele.scionsocket"
	INTERFACE_NAME = "ch.ethz.netsec.flowtele.scionsocket"
	OBJECT_PATH    = "/ch/ethz/netsec/flowtele/scionsocket"
)

type fshaperDbusMethodInterface struct {
	fshaperDbus *FshaperDbus
}

// ApplyControl DEPRECATED!
//func (fshaperDbus fshaperDbusMethodInterface) ApplyControl(dType uint32, flow uint32, flow0 uint64, flow1 uint64, flow2 uint64, flow3 uint64, flow4 uint64, flow5 uint64, flow6 uint64, flow7 uint64, flow8 uint64, flow9 uint64, flow10 uint64) (ret bool, dbusError *dbus.Error) {
//	// apply CC params to QUIC connections
//	if fshaperDbus.fshaperDbus.disabledControl {
//		fshaperDbus.fshaperDbus.Log("IGNORING APPLYCONTROL CALL")
//		return
//	}
//	start := time.Now()
//	flows := []uint64{flow0, flow1, flow2, flow3, flow4, flow5, flow6, flow7, flow8, flow9, flow10}
//	fshaperDbus.fshaperDbus.Log("FShaper received ApplyControl(%d, %d, %+v)", dType, flow, flows)
//	var quicApplyControlDone []chan *dbus.Call
//	for i := 0; i < fshaperDbus.fshaperDbus.nConnections; i++ {
//		quicApplyControlDone = append(quicApplyControlDone, make(chan *dbus.Call, 1))
//	}
//	for i, f := range flows {
//		if i >= fshaperDbus.fshaperDbus.nConnections {
//			break
//		}
//		// TODO: Fix second parameter
//		serviceName := getQuicServiceName(int32(i), fshaperDbus.fshaperDbus.peer)
//		objectPath := getQuicObjectPath(int32(i), fshaperDbus.fshaperDbus.peer)
//		interfaceName := getQuicInterfaceName(int32(i), fshaperDbus.fshaperDbus.peer)
//		obj := fshaperDbus.fshaperDbus.Conn.Object(serviceName, objectPath)
//		fshaperDbus.fshaperDbus.Log("calling ApplyControl on %s in %s at %v", serviceName, objectPath, time.Now().Sub(start))
//
//		var beta float64
//		var cwnd_adjust, cwnd_max_adjust int64
//		var use_conservative_allocation bool
//		beta = float64(int((f>>48)&0xffff)) / 1024
//		cwnd_adjust = int64(int16((f >> 32) & 0xffff))
//		cwnd_max_adjust = int64(int16((f >> 16) & 0xffff))
//		use_conservative_allocation = bool((f & 0x1) == 1)
//
//		// scale up cwnd increase by 2<<10
//		cwnd_adjust = int64(float32(cwnd_adjust) * float32(2<<10))
//		cwnd_max_adjust = int64(float32(cwnd_max_adjust) * float32(2<<10))
//		// call := obj.Call(interfaceName+".ApplyControl", 0, dType, beta, cwnd_adjust, cwnd_max_adjust, use_conservative_allocation)
//		obj.Go(interfaceName+".ApplyControl", 0, quicApplyControlDone[i], dType, beta, cwnd_adjust, cwnd_max_adjust, use_conservative_allocation)
//	}
//	for _, c := range quicApplyControlDone {
//		select {
//		case call := <-c:
//			if call.Err != nil {
//				panic(call.Err)
//			}
//			//fshaperDbus.fshaperDbus.Log("dbus call finished for flow %d finished at %v", i, time.Now().Sub(start))
//			var res bool
//			call.Store(&res)
//			if !res {
//				fshaperDbus.fshaperDbus.Log("FShaper failed to update flow at %v", time.Now().Sub(start))
//				return false, nil
//			}
//		}
//	}
//	fshaperDbus.fshaperDbus.Log("successfully updated flows after %v", time.Now().Sub(start))
//	return true, nil
//}

// ApplyControlV2 expects the connection IDs used by QUIC when the QUIC session was initialized as an string array and the corresponding flow values as a uint64 array
// connIDs are used to identify the correct dbus interfaces
func (fshaperDbus fshaperDbusMethodInterface) ApplyControlV2(dType uint32, flow uint32, connIDs []string, flows []uint64) (ret bool, dbusError *dbus.Error) {
	// apply CC params to QUIC connections
	if fshaperDbus.fshaperDbus.disabledControl {
		fshaperDbus.fshaperDbus.Log("IGNORING APPLYCONTROL CALL")
		return
	}
	start := time.Now()
	fshaperDbus.fshaperDbus.Log("FShaper received ApplyControlV2(%d, %d, %+v, %+v)", dType, flow, connIDs, flows)
	var quicApplyControlDone []chan *dbus.Call
	for i := 0; i < len(connIDs); i++ {
		quicApplyControlDone = append(quicApplyControlDone, make(chan *dbus.Call, 1))
	}

	fmt.Printf("Received %d connIDs and %d flows.\n", len(connIDs), len(flows))

	for i, _ := range connIDs {
		f := flows[i]
		// TODO: Check bounds of connIDs
		serviceName := getQuicServiceName(int32(i), connIDs[i])
		objectPath := getQuicObjectPath(int32(i), connIDs[i])
		interfaceName := getQuicInterfaceName(int32(i), connIDs[i])
		obj := fshaperDbus.fshaperDbus.Conn.Object(serviceName, objectPath)
		fshaperDbus.fshaperDbus.Log("calling ApplyControlV2 on %s in %s at %v", serviceName, objectPath, time.Now().Sub(start))

		var beta float64
		var cwnd_adjust, cwnd_max_adjust int64
		var use_conservative_allocation bool
		beta = float64(int((f>>48)&0xffff)) / 1024
		cwnd_adjust = int64(int16((f >> 32) & 0xffff))
		cwnd_max_adjust = int64(int16((f >> 16) & 0xffff))
		use_conservative_allocation = bool((f & 0x1) == 1)

		// scale up cwnd increase by 2<<10
		cwnd_adjust = int64(float32(cwnd_adjust) * float32(2<<10))
		cwnd_max_adjust = int64(float32(cwnd_max_adjust) * float32(2<<10))
		fmt.Printf("%f %d %d %t\n", beta, cwnd_adjust, cwnd_max_adjust, use_conservative_allocation)

		// call := obj.Call(interfaceName+".ApplyControl", 0, dType, beta, cwnd_adjust, cwnd_max_adjust, use_conservative_allocation)
		obj.Go(interfaceName+".ApplyControl", 0, quicApplyControlDone[i], dType, beta, cwnd_adjust, cwnd_max_adjust, use_conservative_allocation)
		fshaperDbus.fshaperDbus.Log("Call complete to %v", serviceName)
	}
	for _, c := range quicApplyControlDone {
		select {
		case call := <-c:
			if call.Err != nil {
				panic(call.Err)
			}
			//fshaperDbus.fshaperDbus.Log("dbus call finished for flow %d finished at %v", i, time.Now().Sub(start))
			var res bool
			call.Store(&res)
			if !res {
				fshaperDbus.fshaperDbus.Log("FShaper failed to update flow at %v", time.Now().Sub(start))
				return false, nil
			}
		}
	}
	fshaperDbus.fshaperDbus.Log("successfully updated flows after %v", time.Now().Sub(start))
	return true, nil
}

type FlowData struct {
	Beta             float64
	Cwnd_adjust      int64
	Cwnd_max_adjust  int64
	Use_conservative bool
}

// ApplyControlV3 expects the connection IDs used by QUIC when the QUIC session was initialized as an string array and the corresponding flow values as a uint64 array
// connIDs are used to identify the correct dbus interfaces
func (fshaperDbus fshaperDbusMethodInterface) ApplyControlV3(dType uint32, flow uint32, connIDs []string, flows []FlowData) (ret bool, dbusError *dbus.Error) {
	// apply CC params to QUIC connections
	if fshaperDbus.fshaperDbus.disabledControl {
		fshaperDbus.fshaperDbus.Log("IGNORING APPLYCONTROL CALL")
		return
	}
	start := time.Now()
	fshaperDbus.fshaperDbus.Log("FShaper received ApplyControlV3(%d, %d, %+v, %+v)", dType, flow, connIDs, flows)
	var quicApplyControlDone []chan *dbus.Call
	for i := 0; i < len(connIDs); i++ {
		quicApplyControlDone = append(quicApplyControlDone, make(chan *dbus.Call, 1))
	}

	for i, _ := range connIDs {
		serviceName := getQuicServiceName(int32(i), connIDs[i])
		objectPath := getQuicObjectPath(int32(i), connIDs[i])
		interfaceName := getQuicInterfaceName(int32(i), connIDs[i])
		obj := fshaperDbus.fshaperDbus.Conn.Object(serviceName, objectPath)
		fshaperDbus.fshaperDbus.Log("calling ApplyControlV3 on %s in %s at %v", serviceName, objectPath, time.Now().Sub(start))

		f := flows[i]
		beta := f.Beta / 1024
		cwnd_adjust := f.Cwnd_adjust
		cwnd_max_adjust := f.Cwnd_max_adjust
		use_conservative_allocation := f.Use_conservative

		// scale up cwnd increase by 2<<10
		cwnd_adjust = int64(float32(cwnd_adjust) * float32(2<<10))
		cwnd_max_adjust = int64(float32(cwnd_max_adjust) * float32(2<<10))
		// call := obj.Call(interfaceName+".ApplyControl", 0, dType, beta, cwnd_adjust, cwnd_max_adjust, use_conservative_allocation)
		obj.Go(interfaceName+".ApplyControl", 0, quicApplyControlDone[i], dType, beta, cwnd_adjust, cwnd_max_adjust, use_conservative_allocation)
		fshaperDbus.fshaperDbus.Log("Call complete to %v", serviceName)
	}
	for _, c := range quicApplyControlDone {
		select {
		case call := <-c:
			if call.Err != nil {
				panic(call.Err)
			}
			//fshaperDbus.fshaperDbus.Log("dbus call finished for flow %d finished at %v", i, time.Now().Sub(start))
			var res bool
			call.Store(&res)
			if !res {
				fshaperDbus.fshaperDbus.Log("FShaper failed to update flow at %v", time.Now().Sub(start))
				return false, nil
			}
		}
	}
	fshaperDbus.fshaperDbus.Log("successfully updated flows after %v", time.Now().Sub(start))
	return true, nil
}

type FshaperDbus struct {
	DbusBase
	//nConnections    int
	disabledControl bool
	// peer identifies a quic session communicating with another machine which is identified by this string. Can be an IP/port or SCION address
	peer string
}

func NewFshaperDbus(peer string, disableControl bool, fShaperID int) *FshaperDbus {
	var d FshaperDbus
	d.Init()
	d.ServiceName = SERVICE_NAME
	d.ObjectPath = dbus.ObjectPath(OBJECT_PATH)
	d.InterfaceName = INTERFACE_NAME
	if fShaperID != -1 {
		d.ServiceName = fmt.Sprintf("%s%d", d.ServiceName, fShaperID)
		d.ObjectPath = dbus.ObjectPath(fmt.Sprintf("%s%d", d.ObjectPath, fShaperID))
		d.InterfaceName = fmt.Sprintf("%s%d", d.InterfaceName, fShaperID)
	}

	d.peer = peer
	d.LogPrefix = "SOCKET"
	d.ExportedMethods = fshaperDbusMethodInterface{fshaperDbus: &d}
	d.disabledControl = disableControl
	nsString := ""
	elements := strings.Split(string(d.ObjectPath), "/")
	for i := 1; i < len(elements)-1; i++ {
		nsString = nsString + "/" + elements[i]
	}
	namespace := dbus.ObjectPath(nsString)
	d.SignalMatchOptions = []dbus.MatchOption{dbus.WithMatchPathNamespace(namespace)}
	d.ExportedSignals = allFshaperDbusSignals()
	d.LogSignals = false
	return &d
}
