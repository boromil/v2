// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package storage

import "testing"

func TestEscapeFTS5Query(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"2.0.8", `"2.0.8"`},
		{"123", "123"},
		{".config", ".config"},
		{"hello.world", "hello.world"},
		{"", ""},
		{"  ", ""},
		{"go 1.21.3", `go "1.21.3"`},
		{"hello 3.14 world", `hello "3.14" world`},
		{".", "."},
		{"0.1 0.2 42", `"0.1" "0.2" 42`},
		{"v2.0", `"v2.0"`},
	}

	for _, tc := range tests {
		result := escapeFTS5Query(tc.input)
		if result != tc.expected {
			t.Errorf("escapeFTS5Query(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}
