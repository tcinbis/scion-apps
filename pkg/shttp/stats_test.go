package shttp

import (
	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/http3"
	"testing"
)

func TestSHTTPStats_retireClient(t *testing.T) {
	type fields struct {
		clients    map[quic.StatsClientID]*http3.StatusEntry
		oldToNewID map[quic.StatsClientID]quic.StatsClientID
	}
	type args struct {
		cID quic.StatsClientID
	}

	testID := quic.StatsClientID("1234")

	tests := []struct {
		name           string
		fields         fields
		args           args
		wantErr        bool
		expectedStatus http3.Status
	}{
		{
			name: "Retire map test",
			fields: fields{
				clients:    map[quic.StatsClientID]*http3.StatusEntry{testID: http3.NewStatusEntry(testID, nil, nil, http3.Alive)},
				oldToNewID: nil,
			},
			args:           args{cID: "1234"},
			expectedStatus: http3.Retired,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SHTTPStats{
				clients:    tt.fields.clients,
				oldToNewID: tt.fields.oldToNewID,
			}
			if err := s.retireClient(tt.args.cID); (err != nil) != tt.wantErr {
				t.Errorf("retireClient() error = %v, wantErr %v", err, tt.wantErr)
			}

			if stat := tt.fields.clients[testID].Status; stat != tt.expectedStatus {
				t.Errorf("%s has status: %v (expected: %v)", testID, stat, tt.expectedStatus)
			}
		})
	}
}
