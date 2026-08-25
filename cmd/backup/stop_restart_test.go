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

func TestHasLabel(t *testing.T) {
	tests := []struct {
		name          string
		labels        map[string]string
		key           string
		value         string
		matchBehavior MatchBehavior
		expected      bool
	}{
		{
			"match exact",
			map[string]string{"docker-volume-backup.stop-during-backup": "service1"},
			"docker-volume-backup.stop-during-backup",
			"service1",
			"match",
			true,
		},
		{
			"match mismatch",
			map[string]string{"docker-volume-backup.stop-during-backup": "service2"},
			"docker-volume-backup.stop-during-backup",
			"service1",
			"match",
			false,
		},
		{
			"match does not split",
			map[string]string{"docker-volume-backup.stop-during-backup": "service1,service2"},
			"docker-volume-backup.stop-during-backup",
			"service1",
			"match",
			false,
		},
		{
			"one-of first",
			map[string]string{"docker-volume-backup.stop-during-backup": "service1,service2"},
			"docker-volume-backup.stop-during-backup",
			"service1",
			"one-of",
			true,
		},
		{
			"one-of last with spaces",
			map[string]string{"docker-volume-backup.stop-during-backup": "service1, service2"},
			"docker-volume-backup.stop-during-backup",
			"service2",
			"one-of",
			true,
		},
		{
			"one-of no member",
			map[string]string{"docker-volume-backup.stop-during-backup": "service1,service2"},
			"docker-volume-backup.stop-during-backup",
			"service3",
			"one-of",
			false,
		},
		{
			"one-of single value",
			map[string]string{"docker-volume-backup.stop-during-backup": "true"},
			"docker-volume-backup.stop-during-backup",
			"true",
			"one-of",
			true,
		},
		{
			"label absent",
			map[string]string{},
			"docker-volume-backup.stop-during-backup",
			"true",
			"one-of",
			false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := hasLabel(test.labels, test.key, test.value, test.matchBehavior)
			if result != test.expected {
				t.Errorf("Expected %v, got %v", test.expected, result)
			}
		})
	}
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
