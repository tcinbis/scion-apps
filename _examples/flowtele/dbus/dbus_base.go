package flowteledbus

import (
	"fmt"
	"golang.org/x/net/context"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

type DbusBase struct {
	context        context.Context
	ServiceName    string
	ObjectPath     dbus.ObjectPath
	InterfaceName  string
	Conn           *dbus.Conn
	SignalListener chan *dbus.Signal

	ExportedMethods    interface{}
	SignalMatchOptions []dbus.MatchOption
	ExportedSignals    []introspect.Signal

	SignalMinInterval   map[QuicDbusSignalType]time.Duration
	lastSignalSent      map[QuicDbusSignalType]time.Time
	lastSignalSentMutex sync.Mutex

	ackedBytesMutex sync.Mutex
	ackedBytes      uint32

	LogSignals              bool
	SignalLogMinInterval    map[QuicDbusSignalType]time.Duration
	lastSignalLogged        map[QuicDbusSignalType]time.Time
	lastSignalLoggedMutex   sync.Mutex
	logMessagesSkipped      map[QuicDbusSignalType]uint64
	logMessagesSkippedMutex sync.Mutex

	LogPrefix string
}

func (db *DbusBase) Init() {
	db.SignalMinInterval = make(map[QuicDbusSignalType]time.Duration)
	db.lastSignalSent = make(map[QuicDbusSignalType]time.Time)
	db.SignalLogMinInterval = make(map[QuicDbusSignalType]time.Duration)
	db.lastSignalLogged = make(map[QuicDbusSignalType]time.Time)
	db.logMessagesSkipped = make(map[QuicDbusSignalType]uint64)
}

func (db *DbusBase) Acked(ackedBytes uint32) uint32 {
	db.ackedBytesMutex.Lock()
	defer db.ackedBytesMutex.Unlock()
	db.ackedBytes += ackedBytes
	return db.ackedBytes
}

func (db *DbusBase) ResetAcked() {
	db.ackedBytesMutex.Lock()
	defer db.ackedBytesMutex.Unlock()
	db.ackedBytes = 0
}

func (db *DbusBase) SetMinIntervalForAllSignals(interval time.Duration) {
	db.SignalMinInterval[Rtt] = interval
	db.SignalMinInterval[Lost] = interval
	db.SignalMinInterval[Cwnd] = interval
	db.SignalMinInterval[Pacing] = interval
	db.SignalMinInterval[BbrRtt] = interval
	db.SignalMinInterval[BbrBW] = interval
	db.SignalMinInterval[Delivered] = interval
	db.SignalMinInterval[DeliveredAdjust] = interval
	db.SignalMinInterval[GainLost] = interval
}

func (db *DbusBase) SetLogMinIntervalForAllSignals(interval time.Duration) {
	db.SignalLogMinInterval[Rtt] = interval
	db.SignalLogMinInterval[Lost] = interval
	db.SignalLogMinInterval[Cwnd] = interval
	db.SignalLogMinInterval[Pacing] = interval
	db.SignalLogMinInterval[BbrRtt] = interval
	db.SignalLogMinInterval[BbrBW] = interval
	db.SignalLogMinInterval[Delivered] = interval
	db.SignalLogMinInterval[DeliveredAdjust] = interval
	db.SignalLogMinInterval[GainLost] = interval
}

func (db *DbusBase) ShouldSendSignal(s DbusSignal) bool {
	t := s.SignalType()
	interval, ok := db.SignalMinInterval[t]
	if !ok {
		// no min interval is set
		return true
	}
	db.lastSignalSentMutex.Lock()
	lastSignalTime, ok := db.lastSignalSent[t]
	db.lastSignalSentMutex.Unlock()
	now := time.Now()
	if !ok || now.Sub(lastSignalTime) > interval {
		db.lastSignalSentMutex.Lock()
		db.lastSignalSent[t] = now
		db.lastSignalSentMutex.Unlock()
		return true
	} else {
		return false
	}
}

func (db *DbusBase) Send(s DbusSignal) error {
	var logSignal bool
	if db.LogSignals {
		t := s.SignalType()
		interval, ok := db.SignalLogMinInterval[t]
		if !ok {
			// no min interval is set
			logSignal = true
		} else {
			db.lastSignalLoggedMutex.Lock()
			lastSignalLogTime, ok := db.lastSignalLogged[t]
			db.lastSignalLoggedMutex.Unlock()
			now := time.Now()
			if !ok || now.Sub(lastSignalLogTime) > interval {
				db.lastSignalLoggedMutex.Lock()
				db.lastSignalLogged[t] = now
				db.lastSignalLoggedMutex.Unlock()
				logSignal = true
			} else {
				// skip this log message and increase skipped counter
				db.logMessagesSkippedMutex.Lock()
				db.logMessagesSkipped[t] += 1
				db.logMessagesSkippedMutex.Unlock()
			}
		}
		if logSignal {
			nSkipped := uint64(0)
			db.logMessagesSkippedMutex.Lock()
			if val2, ok2 := db.logMessagesSkipped[t]; ok2 {
				nSkipped = val2
			}
			db.logMessagesSkippedMutex.Unlock()
			switch t {
			case Rtt:
				db.Log("RTT (skipped %d) srtt = %.5fms", nSkipped, float32(s.Values()[3].(uint32))/1000)
			case Lost:
				db.Log("Lost (skipped %d) ssthresh = %d", nSkipped, s.Values()[3])
			case Cwnd:
				db.Log("Cwnd (skipped %d) cwnd = %d, inflight = %d, acked = %d", nSkipped, s.Values()[3], s.Values()[4], s.Values()[5])
			}
			db.logMessagesSkippedMutex.Lock()
			db.logMessagesSkipped[t] = 0
			db.logMessagesSkippedMutex.Unlock()
		}
	}
	return db.Conn.Emit(db.ObjectPath, db.InterfaceName+"."+s.Name(), s.Values()...)
}

func (db *DbusBase) OpenSessionBus() error {
	var err error
	// open private session bus for each DbusBase object
	db.Conn, err = dbus.SessionBusPrivate()
	if err != nil {
		return fmt.Errorf("Failed to connect to session bus: %s", err)
	}

	// initialize session bus (is done automatically in dbus.SessionBus())
	if err = db.Conn.Auth(nil); err != nil {
		_ = db.Conn.Close()
		return err
	}
	if err = db.Conn.Hello(); err != nil {
		_ = db.Conn.Close()
		return err
	}

	reply, err := db.Conn.RequestName(db.ServiceName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return fmt.Errorf("Failed to request name: %s", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("Name (%s) already taken", db.ServiceName)
	}
	return nil
}

func (db *DbusBase) Close() {
	db.Conn.Close()
	db.Conn = nil
}

func (db *DbusBase) registerMethods() error {
	return db.Conn.Export(db.ExportedMethods, db.ObjectPath, db.InterfaceName)
}

func (db *DbusBase) registerSignalListeners() error {
	if err := db.Conn.AddMatchSignal(
		db.SignalMatchOptions...,
	// dbus.WithMatchObjectPath(db.ObjectPath),
	// dbus.WithMatchInterface(db.InterfaceName),
	// dbus.WithMatchMember("mysignal"),
	// dbus.WithMatchSender(REMOTE_SERVICE_NAME),
	); err != nil {
		return err
	}

	// todo(cyrill) adjust the max number of buffered signals (maybe n_connection*n_signal_types to
	// allow one signal from each connection to be buffered?)
	db.SignalListener = make(chan *dbus.Signal, 1)
	db.Conn.Signal(db.SignalListener)
	return nil
}

func (db *DbusBase) registerIntrospectMethod() error {
	// fmt.Printf("Name: %s, methods: %+v\n", db.InterfaceName, introspect.Methods(db.ExportedMethods))
	n := &introspect.Node{
		Name: string(db.ObjectPath),
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			{
				Name:    db.InterfaceName,
				Methods: introspect.Methods(db.ExportedMethods),
				Signals: db.ExportedSignals,
			},
		},
	}
	return db.Conn.Export(introspect.NewIntrospectable(n), db.ObjectPath, "org.freedesktop.DBus.Introspectable")
}

func (db *DbusBase) Register() error {
	if err := db.registerMethods(); err != nil {
		return err
	}
	if err := db.registerSignalListeners(); err != nil {
		return err
	}
	if err := db.registerIntrospectMethod(); err != nil {
		return err
	}
	return nil
}

func (db *DbusBase) Log(formatString string, args ...interface{}) {
	fmt.Printf(db.LogPrefix+": "+formatString+"\n", args...)
}

func (db *DbusBase) observeContext() {
	for {
		select {
		case <-db.context.Done():
			fmt.Println("Closing QBUS.")
			db.Close()
			return
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}
