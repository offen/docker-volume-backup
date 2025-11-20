package main

import (
	"context"
	"errors"
	"testing"

	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"
)

type mockInfoClient struct {
	result client.SystemInfoResult
	err    error
}

func (m *mockInfoClient) Info(context.Context, client.InfoOptions) (client.SystemInfoResult, error) {
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
				result: client.SystemInfoResult{
					Info: system.Info{
						Swarm: swarm.Info{
							LocalNodeState:   swarm.LocalNodeStateActive,
							ControlAvailable: true,
						},
					},
				},
			},
			true,
			false,
		},
		{
			"worker",
			&mockInfoClient{
				result: client.SystemInfoResult{
					Info: system.Info{
						Swarm: swarm.Info{
							LocalNodeState: swarm.LocalNodeStateActive,
						},
					},
				},
			},
			false,
			false,
		},
		{
			"compose",
			&mockInfoClient{
				result: client.SystemInfoResult{
					Info: system.Info{
						Swarm: swarm.Info{
							LocalNodeState: swarm.LocalNodeStateInactive,
						},
					},
				},
			},
			false,
			false,
		},
		{
			"balena",
			&mockInfoClient{
				result: client.SystemInfoResult{
					Info: system.Info{
						Swarm: swarm.Info{
							LocalNodeState: "",
						},
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
