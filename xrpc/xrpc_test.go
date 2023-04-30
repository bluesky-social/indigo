package xrpc

import (
	"testing"
)

// TestMakeParams tests the makeParams function.
func TestMakeParams(t *testing.T) {
	testCases := []struct {
		name     string
		input    map[string]interface{}
		expected string
	}{
		{
			name:     "Empty input",
			input:    map[string]interface{}{},
			expected: "",
		},
		{
			name: "Single value",
			input: map[string]interface{}{
				"key": "value",
			},
			expected: "key=value",
		},
		{
			name: "Multiple values",
			input: map[string]interface{}{
				"key1": "value1",
				"key2": "value2",
			},
			expected: "key1=value1&key2=value2",
		},
		{
			name: "Slice of strings",
			input: map[string]interface{}{
				"key": []string{"value1", "value2", "value3"},
			},
			expected: "key=value1%2Cvalue2%2Cvalue3",
		},
		{
			name: "Mixed values",
			input: map[string]interface{}{
				"key1": "value1",
				"key2": []string{"value2", "value3"},
			},
			expected: "key1=value1&key2=value2%2Cvalue3",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := makeParams(tc.input)
			if result != tc.expected {
				t.Errorf("Expected '%s', got '%s'", tc.expected, result)
			}
		})
	}
}

func TestXRPC(t *testing.T) {
	// Run the tests
	TestMakeParams(t)
}
