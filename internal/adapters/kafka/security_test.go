package kafka

import "testing"

func TestSecurityConfigDialer(t *testing.T) {
	tests := []struct {
		name    string
		config  SecurityConfig
		wantErr bool
	}{
		{name: "local plaintext defaults"},
		{name: "tls only", config: SecurityConfig{TLS: true}},
		{name: "scram sha 256", config: SecurityConfig{TLS: true, Mechanism: "SCRAM-SHA-256", Username: "user", Password: "secret"}},
		{name: "scram sha 512", config: SecurityConfig{TLS: true, Mechanism: "scram-sha-512", Username: "user", Password: "secret"}},
		{name: "sasl without tls", config: SecurityConfig{Mechanism: "SCRAM-SHA-512", Username: "user", Password: "secret"}, wantErr: true},
		{name: "incomplete credentials", config: SecurityConfig{TLS: true, Mechanism: "SCRAM-SHA-512", Username: "user"}, wantErr: true},
		{name: "unsupported mechanism", config: SecurityConfig{TLS: true, Mechanism: "PLAIN", Username: "user", Password: "secret"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialer, err := tt.config.Dialer()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Dialer() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && tt.config.TLS && dialer.TLS == nil {
				t.Fatal("TLS configuration was not created")
			}
			if err == nil && tt.config.Mechanism != "" && dialer.SASLMechanism == nil {
				t.Fatal("SASL mechanism was not created")
			}
		})
	}
}
