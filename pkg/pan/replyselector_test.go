package pan

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/scionproto/scion/go/lib/addr"
	"reflect"
	"testing"
	"time"
)

func Test_remoteEntry_MarshalJSON(t *testing.T) {
	type fields struct {
		paths       pathsMRU
		seen        time.Time
		expireTimer *time.Timer
		expired     func()
	}
	startTime := time.Now()
	tests := []struct {
		name    string
		fields  fields
		want    []byte
		wantErr bool
	}{
		{
			name: "Default",
			fields: fields{
				paths:       pathsMRU{},
				seen:        startTime,
				expireTimer: nil,
				expired:     nil,
			},
			want:    []byte(fmt.Sprintf("{\"paths_used\":0,\"last_seen\":%d}", startTime.Unix())),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &RemoteEntry{
				paths:       tt.fields.paths,
				seen:        tt.fields.seen,
				expireTimer: tt.fields.expireTimer,
				expired:     tt.fields.expired,
			}
			got, err := u.MarshalJSON()
			if (err != nil) != tt.wantErr {
				t.Errorf("MarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MarshalJSON() got = %v, want %v", string(got), string(tt.want))
			}
		})
	}
}

func Test_udpAddrKey_MarshalJson(t *testing.T) {
	type fields struct {
		IA   addr.IA
		IP   [16]byte
		Port int
	}

	address, err := DummyAddr()
	if err != nil {
		t.Errorf("%v.\n", err)
		return
	}
	var ip [16]byte
	copy(ip[:], address.IP.To16())

	tests := []struct {
		name    string
		fields  fields
		want    []byte
		wantErr bool
	}{
		{
			name: "Test 1",
			fields: fields{
				IA:   address.IA,
				IP:   ip,
				Port: 8080,
			},
			want:    []byte("\"1-ff00:0:300,[192.168.1.1:8080]\""),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &UdpAddrKey{
				IA:   tt.fields.IA,
				IP:   tt.fields.IP,
				Port: tt.fields.Port,
			}
			got, err := json.Marshal(u)
			if (err != nil) != tt.wantErr {
				t.Errorf("MarshalJson() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MarshalJson() got = %v, want %v", string(got), string(tt.want))
			}
		})
	}
}

func Test_mapUdpAddrKeyRemotes__MarshalJson(t *testing.T) {
	address, err := DummyAddr()
	if err != nil {
		t.Errorf("%v.\n", err)
		return
	}

	startTime := time.Now()
	remotes := make(map[UdpAddrKey]RemoteEntry)
	remotes[makeKey(address)] = RemoteEntry{
		paths:       pathsMRU{},
		seen:        startTime,
		expireTimer: nil,
		expired:     nil,
	}

	tests := []struct {
		name    string
		remote  map[UdpAddrKey]RemoteEntry
		want    []byte
		wantErr bool
	}{
		{
			name:    "Test 1",
			remote:  remotes,
			want:    []byte(fmt.Sprintf("{\"1-ff00:0:300,[192.168.1.1:8080]\":{\"paths_used\":%d,\"last_seen\":%d}}", 0, int(startTime.Unix()))),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &tt.remote
			fmt.Printf("%v\n", u)
			got, err := json.Marshal(u)
			if (err != nil) != tt.wantErr {
				t.Errorf("Marshal() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Marshal() got = %v, want %v", string(got), string(tt.want))
			}
		})
	}
}

func TestMultiReplySelector_ToJSON(t *testing.T) {
	sel := DummyMultiReplySelector()
	tests := []struct {
		name     string
		selector *MultiReplySelector
		want     []byte
		wantErr  bool
	}{
		{
			name:     "Test1",
			selector: sel,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.selector)
			if (err != nil) != tt.wantErr {
				t.Errorf("Marshal() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			var js map[string]interface{}
			if err := json.Unmarshal(got, &js); err != nil {
				t.Errorf("Invalid JSON. %v", err)
			}
		})
	}
}

func DummyAddr() (UDPAddr, error) {
	return ParseUDPAddr("1-ff00:0:300,192.168.1.1:8080")
}

func DummyMultiReplySelector() *MultiReplySelector {
	rAddr, _ := DummyAddr()

	rCtx, rCancel := context.WithCancel(context.Background())
	s := &MultiReplySelector{
		Remotes:    make(map[UdpAddrKey]RemoteEntry),
		ctx:        rCtx,
		cancel:     rCancel,
		ticker:     time.NewTicker(10 * time.Second),
		useUpdates: false,
		IaRemotes:  make(map[addr.IA][]UdpAddrKey),
		IaPaths:    make(map[addr.IA][]*Path),
	}

	s.OnPacketReceived(rAddr, UDPAddr{}, &Path{
		Source:         rAddr.IA,
		Destination:    rAddr.IA,
		ForwardingPath: ForwardingPath{},
		Metadata:       nil,
		Fingerprint:    "1234",
		Expiry:         time.Now().Add(5 * time.Minute),
	})

	return s
}
