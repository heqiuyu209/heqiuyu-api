package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReplaceContentValues(t *testing.T) {
	tests := []struct {
		name    string
		content string
		values  []interface{}
		want    string
	}{
		{
			name:    "replaces placeholders in order",
			content: "{{value}} used {{value}} units; open {{value}}",
			values:  []interface{}{"alice", 42, "https://example.com"},
			want:    "alice used 42 units; open https://example.com",
		},
		{
			name:    "keeps unmatched placeholders",
			content: "{{value}} / {{value}}",
			values:  []interface{}{"first"},
			want:    "first / {{value}}",
		},
		{
			name:    "ignores extra values",
			content: "no placeholders",
			values:  []interface{}{"unused"},
			want:    "no placeholders",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, ReplaceContentValues(test.content, test.values))
		})
	}
}
