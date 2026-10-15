package app

import "testing"

func TestDatabaseTLSVerifyRequiresVerifyFull(t *testing.T) {
	tests := []struct {
		name, databaseURL string
		wantErr           bool
	}{
		{name: "verified tls", databaseURL: "postgres://user:pass@db.example:5432/app?sslmode=verify-full"},
		{name: "unverified tls rejected", databaseURL: "postgres://user:pass@db.example:5432/app?sslmode=require", wantErr: true},
		{name: "plaintext rejected", databaseURL: "postgres://user:pass@db.example:5432/app?sslmode=disable", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", tt.databaseURL)
			t.Setenv("DATABASE_TLS_VERIFY", "true")
			t.Setenv("KAFKA_TLS", "false")
			t.Setenv("KAFKA_SASL_MECHANISM", "")
			t.Setenv("KAFKA_SASL_USERNAME", "")
			t.Setenv("KAFKA_SASL_PASSWORD", "")
			_, err := ConfigFromEnv()
			if (err != nil) != tt.wantErr {
				t.Fatalf("ConfigFromEnv() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
