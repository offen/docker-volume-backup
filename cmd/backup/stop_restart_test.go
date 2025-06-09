package main

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/system"
)

type mockInfoClient struct {
	result system.Info
	err    error
}

func (m *mockInfoClient) Info(context.Context) (system.Info, error) {
	return m.result, m.err
}

func TestIsSwarm(t *testing.T) {
	tests := []struct {
		name        string
		client      *mockInfoClient
		expected    bool
		expectError bool
	}{
		{
			"swarm",
			&mockInfoClient{
				result: system.Info{
					Swarm: swarm.Info{
						LocalNodeState:   swarm.LocalNodeStateActive,
						ControlAvailable: true,
					},
				},
			},
			true,
			false,
		},
		{
			"worker",
			&mockInfoClient{
				result: system.Info{
					Swarm: swarm.Info{
						LocalNodeState: swarm.LocalNodeStateActive,
					},
				},
			},
			false,
			false,
		},
		{
			"compose",
			&mockInfoClient{
				result: system.Info{
					Swarm: swarm.Info{
						LocalNodeState: swarm.LocalNodeStateInactive,
					},
				},
			},
			false,
			false,
		},
		{
			"balena",
			&mockInfoClient{
				result: system.Info{
					Swarm: swarm.Info{
						LocalNodeState: "",
					},
				},
			},
			false,
			false,
		},
		{
			"error",
			&mockInfoClient{
				err: errors.New("the dinosaurs escaped"),
			},
			false,
			true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := isSwarm(test.client)
			if (err != nil) != test.expectError {
				t.Errorf("Unexpected error value %v", err)
			}
			if test.expected != result {
				t.Errorf("Expected %v, got %v", test.expected, result)
			}
		})
	}
}
