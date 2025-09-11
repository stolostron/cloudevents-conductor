package grpc

import (
	"math"
	"os"
	"reflect"
	"testing"
	"time"

	dbconfig "github.com/openshift-online/maestro/pkg/config"
	"github.com/openshift-online/maestro/pkg/constants"
	"github.com/stretchr/testify/assert"
	grpcserver "open-cluster-management.io/sdk-go/pkg/server/grpc"
)

// Test loadGRPCServerConfig with valid config file
func TestLoadGRPCServerConfig(t *testing.T) {
	// table driven tests for loadGRPCServerConfig
	cases := []struct {
		name          string
		configContent string
		expected      *GRPCServerConfig
		expectError   bool
	}{
		{
			name: "ValidConfig",
			configContent: `
grpc_config:
  tls_cert_file: "/path/to/tls.crt"
  tls_key_file: "/path/to/tls.key"
  client_ca_file: "/path/to/ca.crt"
db_config:
  host: "localhost"
  port: 5432
  name: "foo"
  username: "bar"
  password: "goo"
  sslmode: "disable"
`,
			expected: &GRPCServerConfig{
				GRPCConfig: &grpcserver.GRPCServerOptions{
					ServerBindPort:        "8090",
					MaxConcurrentStreams:  math.MaxUint32,
					MaxReceiveMessageSize: 1024 * 1024 * 4,
					MaxSendMessageSize:    math.MaxInt32,
					ConnectionTimeout:     120 * time.Second,
					MaxConnectionAge:      time.Duration(math.MaxInt64),
					ClientMinPingInterval: 5 * time.Second,
					ServerPingInterval:    30 * time.Second,
					ServerPingTimeout:     10 * time.Second,
					WriteBufferSize:       32 * 1024,
					ReadBufferSize:        32 * 1024,
					TLSCertFile:           "/path/to/tls.crt",
					TLSKeyFile:            "/path/to/tls.key",
					ClientCAFile:          "/path/to/ca.crt",
					TLSMinVersion:         771,
					TLSMaxVersion:         772,
				},
				DBConfig: &dbconfig.DatabaseConfig{
					AuthMethod:         constants.AuthMethodPassword,
					TokenRequestScope:  "https://ossrdbms-aad.database.windows.net/.default",
					Dialect:            "postgres",
					Debug:              false,
					MaxOpenConnections: 50,
					HostFile:           "secrets/db.host",
					PortFile:           "secrets/db.port",
					NameFile:           "secrets/db.name",
					UsernameFile:       "secrets/db.user",
					PasswordFile:       "secrets/db.password",
					RootCertFile:       "secrets/db.rootcert",
					Host:               "localhost",
					Port:               5432,
					Name:               "foo",
					Username:           "bar",
					Password:           "goo",
					SSLMode:            "disable",
				},
			},
			expectError: false,
		},
		{
			name:          "InvalidConfig",
			configContent: `:invalid yaml`,
			expected:      nil,
			expectError:   true,
		},
		{
			name:          "EmptyConfig",
			configContent: ``,
			expected: &GRPCServerConfig{
				GRPCConfig: &grpcserver.GRPCServerOptions{
					ServerBindPort:        "8090",
					MaxConcurrentStreams:  math.MaxUint32,
					MaxReceiveMessageSize: 1024 * 1024 * 4,
					MaxSendMessageSize:    math.MaxInt32,
					ConnectionTimeout:     120 * time.Second,
					MaxConnectionAge:      time.Duration(math.MaxInt64),
					ClientMinPingInterval: 5 * time.Second,
					ServerPingInterval:    30 * time.Second,
					ServerPingTimeout:     10 * time.Second,
					WriteBufferSize:       32 * 1024,
					ReadBufferSize:        32 * 1024,
					TLSCertFile:           "/var/run/secrets/hub/grpc/serving-cert/tls.crt",
					TLSKeyFile:            "/var/run/secrets/hub/grpc/serving-cert/tls.key",
					ClientCAFile:          "/var/run/secrets/hub/grpc/ca/ca-bundle.crt",
					TLSMinVersion:         771,
					TLSMaxVersion:         772,
				},
				DBConfig: &dbconfig.DatabaseConfig{
					AuthMethod:         constants.AuthMethodPassword,
					TokenRequestScope:  "https://ossrdbms-aad.database.windows.net/.default",
					Dialect:            "postgres",
					SSLMode:            "disable",
					Debug:              false,
					MaxOpenConnections: 50,
					HostFile:           "secrets/db.host",
					PortFile:           "secrets/db.port",
					NameFile:           "secrets/db.name",
					UsernameFile:       "secrets/db.user",
					PasswordFile:       "secrets/db.password",
					RootCertFile:       "secrets/db.rootcert",
				},
			},
			expectError: false,
		},
		{
			name: "MissingGRPCConfig",
			configContent: `
db_config:
  host: "localhost"
  port: 5432
  name: "foo"
  username: "bar"
  password: "goo"
  sslmode: "disable"
`,
			expected: &GRPCServerConfig{
				GRPCConfig: &grpcserver.GRPCServerOptions{
					ServerBindPort:        "8090",
					MaxConcurrentStreams:  math.MaxUint32,
					MaxReceiveMessageSize: 1024 * 1024 * 4,
					MaxSendMessageSize:    math.MaxInt32,
					ConnectionTimeout:     120 * time.Second,
					MaxConnectionAge:      time.Duration(math.MaxInt64),
					ClientMinPingInterval: 5 * time.Second,
					ServerPingInterval:    30 * time.Second,
					ServerPingTimeout:     10 * time.Second,
					WriteBufferSize:       32 * 1024,
					ReadBufferSize:        32 * 1024,
					TLSCertFile:           "/var/run/secrets/hub/grpc/serving-cert/tls.crt",
					TLSKeyFile:            "/var/run/secrets/hub/grpc/serving-cert/tls.key",
					ClientCAFile:          "/var/run/secrets/hub/grpc/ca/ca-bundle.crt",
					TLSMinVersion:         771,
					TLSMaxVersion:         772,
				},
				DBConfig: &dbconfig.DatabaseConfig{
					AuthMethod:         constants.AuthMethodPassword,
					TokenRequestScope:  "https://ossrdbms-aad.database.windows.net/.default",
					Dialect:            "postgres",
					Debug:              false,
					MaxOpenConnections: 50,
					HostFile:           "secrets/db.host",
					PortFile:           "secrets/db.port",
					NameFile:           "secrets/db.name",
					UsernameFile:       "secrets/db.user",
					PasswordFile:       "secrets/db.password",
					RootCertFile:       "secrets/db.rootcert",
					Host:               "localhost",
					Port:               5432,
					Name:               "foo",
					Username:           "bar",
					Password:           "goo",
					SSLMode:            "disable",
				},
			},
			expectError: false,
		},
		{
			name: "MissingDBConfig",
			configContent: `
grpc_config:
  tls_cert_file: "/path/to/tls.crt"
  tls_key_file: "/path/to/tls.key"
  client_ca_file: "/path/to/ca.crt"
`,
			expected: &GRPCServerConfig{
				GRPCConfig: &grpcserver.GRPCServerOptions{
					ServerBindPort:        "8090",
					MaxConcurrentStreams:  math.MaxUint32,
					MaxReceiveMessageSize: 1024 * 1024 * 4,
					MaxSendMessageSize:    math.MaxInt32,
					ConnectionTimeout:     120 * time.Second,
					MaxConnectionAge:      time.Duration(math.MaxInt64),
					ClientMinPingInterval: 5 * time.Second,
					ServerPingInterval:    30 * time.Second,
					ServerPingTimeout:     10 * time.Second,
					WriteBufferSize:       32 * 1024,
					ReadBufferSize:        32 * 1024,
					TLSCertFile:           "/path/to/tls.crt",
					TLSKeyFile:            "/path/to/tls.key",
					ClientCAFile:          "/path/to/ca.crt",
					TLSMinVersion:         771,
					TLSMaxVersion:         772,
				},
				DBConfig: &dbconfig.DatabaseConfig{
					AuthMethod:         constants.AuthMethodPassword,
					TokenRequestScope:  "https://ossrdbms-aad.database.windows.net/.default",
					Dialect:            "postgres",
					SSLMode:            "disable",
					Debug:              false,
					MaxOpenConnections: 50,
					HostFile:           "secrets/db.host",
					PortFile:           "secrets/db.port",
					NameFile:           "secrets/db.name",
					UsernameFile:       "secrets/db.user",
					PasswordFile:       "secrets/db.password",
					RootCertFile:       "secrets/db.rootcert",
				},
			},
			expectError: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a temporary file with the config content
			tmpFile, err := os.CreateTemp("", "grpc_config_*.yaml")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.WriteString(tc.configContent); err != nil {
				t.Fatalf("Failed to write to temp file: %v", err)
			}
			tmpFile.Close()

			// Load the config
			config, err := loadGRPCServerConfig(tmpFile.Name())
			if tc.expectError {
				assert.NotNil(t, err, "Expected error but got none")
				return
			}

			assert.Nil(t, err, "Expected no error but got: %v", err)
			// Compare the values, not the pointers
			if tc.expected == nil {
				if config != nil {
					t.Errorf("Expected nil config, got: %+v", config)
				}
			} else {
				if config == nil {
					t.Errorf("Expected config, got nil")
				} else {
					if !reflect.DeepEqual(*tc.expected.GRPCConfig, *config.GRPCConfig) {
						t.Errorf("Loaded GRPCConfig does not match expected:\nExpected: %+v\nGot: %+v", *tc.expected.GRPCConfig, *config.GRPCConfig)
					}
					if !reflect.DeepEqual(*tc.expected.DBConfig, *config.DBConfig) {
						t.Errorf("Loaded DBConfig does not match expected:\nExpected: %+v\nGot: %+v", *tc.expected.DBConfig, *config.DBConfig)
					}
				}
			}
		})
	}
}
