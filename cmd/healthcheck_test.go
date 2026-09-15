package cmd

import (
	"reflect"
	"testing"
)

// An empty webListen means every interface and the check runs beside the panel,
// so loopback is what it can reach -- both families, since a panel bound to "::"
// may have no IPv4 loopback socket at all.
//
// A configured address is dialled as given. Assuming loopback there would report
// a healthy panel as dead, and an orchestrator would kill it for that.
func TestHealthCheckHosts(t *testing.T) {
	tests := []struct {
		listen string
		want   []string
	}{
		{"", []string{"127.0.0.1", "::1"}},
		{"0.0.0.0", []string{"127.0.0.1", "::1"}},
		{"::", []string{"127.0.0.1", "::1"}},
		{"127.0.0.1", []string{"127.0.0.1"}},
		{"10.0.0.5", []string{"10.0.0.5"}},
		{"fd00::1", []string{"fd00::1"}},
	}

	for _, tt := range tests {
		t.Run(tt.listen, func(t *testing.T) {
			if got := healthCheckHosts(tt.listen); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("healthCheckHosts(%q) = %v, want %v", tt.listen, got, tt.want)
			}
		})
	}
}
