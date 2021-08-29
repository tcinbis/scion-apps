package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"strings"

	"github.com/godbus/dbus/v5"
	flowteledbus "github.com/netsec-ethz/scion-apps/_examples/flowtele/dbus"
)

type AthenaStruct struct {
	Users      []AthenaFlow `json:"users"`
	Calibrator string       `json:"calibrator"`
}

type AthenaFlow struct {
	ID     int     `json:"id"`
	QuicID string  `json:"quic_id"`
	Addr   string  `json:"addr"`
	Port   int     `json:"port"`
	Weight float64 `json:"weight"`
}

func dbusIDExport(path string) error {
	conn, err := dbus.SessionBus()
	if err != nil {
		return fmt.Errorf("Failed to connect to session bus:", err)
	}
	defer conn.Close()

	var list []string
	err = conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&list)
	if err != nil {
		return err
	}

	var ids []string
	for _, v := range list {
		if strings.HasPrefix(v, flowteledbus.QUIC_SERVICE_NAME) {
			matches := regexp.MustCompile("_[\\d\\w]+").FindAllString(v, -1)
			if matches == nil {
				fmt.Printf("No id found in \"%s\"!", v)
				continue
			}
			for _, id := range matches {
				ids = append(ids, strings.Replace(id, "_", "", -1))
			}
		}
	}
	fmt.Printf("Found %d quic IDs\n", len(ids))

	return writeJson(athenaJsonExport(ids), path)
}

func athenaJsonExport(ids []string) AthenaStruct {
	n_ids := len(ids)
	athena := AthenaStruct{
		Users:      make([]AthenaFlow, 0, n_ids-1),
		Calibrator: ids[n_ids-1],
	}
	prioWeight := 0.8
	donatorWeight := (1 - prioWeight) / float64(n_ids-2)
	for i := 0; i < n_ids-1; i++ {
		weight := donatorWeight
		if i == 0 {
			weight = prioWeight
		}

		athena.Users = append(athena.Users, AthenaFlow{
			ID:     i,
			QuicID: ids[i],
			Addr:   "42.42.42.42",
			Port:   rand.Intn(9999),
			Weight: weight,
		})
	}
	return athena
}

func writeJson(obj interface{}, filepath string) error {
	var res []byte
	var err error

	if eObj, ok := obj.(interface{ Export() ([]byte, error) }); ok {
		res, err = eObj.Export()
	} else {
		res, err = json.MarshalIndent(obj, "", "\t")
	}
	if err != nil {
		return err
	}

	f, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer f.Close()

	f.Truncate(0)
	_, err = f.Seek(0, 0)
	if err != nil {
		return err
	}

	w := bufio.NewWriter(f)
	_, err = w.Write(res)
	if err != nil {
		return err
	}

	w.Flush()
	return nil
}
