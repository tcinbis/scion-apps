package flowteledbus

import (
	"fmt"
	"github.com/godbus/dbus/v5"
	"os"
	"testing"
	"time"
)

const (
	connIDOne = "db67sj"
	connIDTwo = "ab5s8o"
)

func TestDbusConnection(t *testing.T) {
	conn, err := dbus.SessionBus()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to connect to session bus:", err)
		os.Exit(1)
	}
	defer conn.Close()
	var list []string

	err = conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&list)
	if err != nil {
		panic(err)
	}
	for _, v := range list {
		fmt.Println(v)
	}
}

func TestQuicDbusSetup(t *testing.T) {
	qdbus := startRegisterQuic(connIDTwo)
	defer qdbus.Close()

	var list []string
	err := qdbus.Conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&list)
	if err != nil {
		panic(err)
	}
	for _, v := range list {
		fmt.Println(v)
	}

	time.Sleep(300 * time.Second)
}

func startRegisterQuic(connID string) *QuicDbus {
	qdbus := NewQuicDbus(0, false, connID)
	if err := qdbus.OpenSessionBus(); err != nil {
		panic(err)
	}
	if err := qdbus.Register(); err != nil {
		panic(err)
	}
	return qdbus
}

func startRegisterFshaper(connID string) *FshaperDbus {
	fdbus := NewFshaperDbus(1, connID)

	// dbus setup
	if err := fdbus.OpenSessionBus(); err != nil {
		panic(err)
	}
	if err := fdbus.Register(); err != nil {
		panic(err)
	}

	return fdbus
}

func TestFShaperConnection(t *testing.T) {
	qdbus := startRegisterQuic(connIDOne)
	startRegisterFshaper(connIDOne)
	time.Sleep(1 * time.Second)
	var res interface{}

	err := qdbus.Conn.Object("ch.ethz.netsec.flowtele.scionsocket", "/ch/ethz/netsec/flowtele/scionsocket").Call("ApplyControl", 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0).Store(res)
	if err != nil {
		fmt.Println(res)
		panic(err)
	}
}
