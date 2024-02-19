package main

import (
	"os"
	"reflect"
	"testing"
)

func TestSource(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectError    bool
		expectedOutput map[string]string
	}{
		{
			"default",
			"testdata/default.env",
			false,
			map[string]string{
				"FOO": "bar",
				"BAZ": "qux",
			},
		},
		{
			"not found",
			"testdata/nope.env",
			true,
			nil,
		},
		{
			"braces",
			"testdata/braces.env",
			false,
			map[string]string{
				"FOO": "${bar:-qux}",
				"BAR": "xxx",
			},
		},
		{
			"expansion",
			"testdata/expansion.env",
			false,
			map[string]string{
				"BAR": "xxx",
				"FOO": "xxx",
				"BAZ": "xxx",
				"QUX": "yyy",
			},
		},
	}

	os.Setenv("QUX", "yyy")
	defer os.Unsetenv("QUX")

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := source(test.input)
			if (err != nil) != test.expectError {
				t.Errorf("Unexpected error value %v", err)
			}
			if !reflect.DeepEqual(test.expectedOutput, result) {
				t.Errorf("Expected %v, got %v", test.expectedOutput, result)
			}
		})
	}
}
