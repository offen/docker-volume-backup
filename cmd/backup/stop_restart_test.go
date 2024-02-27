package main

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"
)

type mockInfoClient struct {
	result types.Info
	err    error
}

func (m *mockInfoClient) Info(context.Context) (types.Info, error) {
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
				result: types.Info{
					Swarm: swarm.Info{
						LocalNodeState: swarm.LocalNodeStateActive,
					},
				},
			},
			true,
			false,
		},
		{
			"compose",
			&mockInfoClient{
				result: types.Info{
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
				result: types.Info{
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
