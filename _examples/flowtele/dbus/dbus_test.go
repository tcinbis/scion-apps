package flowteledbus

import (
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
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

	time.Sleep(100 * time.Second)
}

func TestManyQuicDbusSetup(t *testing.T) {
	for i := 0; i <= 5; i++ {
		qdbus := startRegisterQuic("")
		defer qdbus.Close()
	}

	time.Sleep(1000 * time.Second)
}

func RandStringBytes(n int) string {
	const signs = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = signs[rand.Intn(len(signs))]
	}
	return string(b)
}

func startRegisterQuic(connID string) *QuicDbus {
	if connID == "" {
		connID = RandStringBytes(8)
	}
	qdbus := NewQuicDbus(0, false, connID, fmt.Sprintf("17-ffaa:1:f12,127.0.0.1:%d", 1000+rand.Intn(1000)))
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
