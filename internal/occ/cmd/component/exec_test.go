// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package component

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"

	"github.com/openchoreo/openchoreo/internal/occ/resources/client"
)

func TestBuildExecWebSocketURL(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		params   ExecParams
		wantPath string
		wantKeys []string // query keys that must be present
	}{
		{
			name: "basic with namespace and component",
			base: "http://localhost:8080",
			params: ExecParams{
				Namespace: "default",
				Component: "my-service",
			},
			wantPath: "/exec/namespaces/default/components/my-service",
		},
		{
			name: "https converts to wss",
			base: "https://api.example.com",
			params: ExecParams{
				Namespace: "ns",
				Component: "comp",
			},
			wantPath: "/exec/namespaces/ns/components/comp",
		},
		{
			name: "all flags set",
			base: "http://localhost:8080",
			params: ExecParams{
				Namespace:   "acme",
				Project:     "store",
				Component:   "api",
				Environment: "dev",
				Container:   "app",
				TTY:         true,
				Stdin:       true,
				Command:     []string{"echo", "hello world"},
			},
			wantPath: "/exec/namespaces/acme/components/api",
			wantKeys: []string{"project", "env", "container", "tty", "stdin", "command"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildExecWebSocketURL(tt.base, tt.params)
			assert.NoError(t, err)
			assert.Contains(t, got, tt.wantPath)
			for _, key := range tt.wantKeys {
				assert.Contains(t, got, key+"=")
			}
		})
	}
}

func TestNewExecCmdArgParsing(t *testing.T) {
	// Test that the exec command parses -- separator correctly.
	// We construct the command but don't execute RunE (it would need a real API).

	tests := []struct {
		name        string
		args        []string
		wantCompArg string
	}{
		{
			name:        "component name only",
			args:        []string{"my-service"},
			wantCompArg: "my-service",
		},
		{
			name:        "component with -- separator",
			args:        []string{"my-service", "--", "ls"},
			wantCompArg: "my-service",
		},
		{
			name:        "component with multi-word command after --",
			args:        []string{"my-service", "--", "echo", "hello", "world"},
			wantCompArg: "my-service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newExecCmd(func() (client.Interface, error) { return nil, nil })
			cmd.PreRunE = nil // skip auth check in tests
			cmd.RunE = nil    // prevent execution
			cmd.Run = func(_ *cobra.Command, _ []string) {}
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			assert.NoError(t, err)
		})
	}
}
