package claudemessages

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeMessagesRequestToOpenAIChatForcesStreamUsage(t *testing.T) {
	streamTrue := true
	streamFalse := false
	tests := []struct {
		name        string
		stream      *bool
		wantOptions *dto.StreamOptions
	}{
		// Anthropic Messages requests carry no stream_options, so a streaming
		// conversion must always come out with include_usage injected.
		{name: "stream injects include_usage", stream: &streamTrue, wantOptions: &dto.StreamOptions{IncludeUsage: true}},
		{name: "non-stream stays nil", stream: &streamFalse},
		{name: "nil stream stays nil", stream: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := dto.ClaudeRequest{
				Model:     "deepseek-v4-flash",
				MaxTokens: loPtr(uint(128)),
				Stream:    tt.stream,
			}

			got, err := ClaudeMessagesRequestToOpenAIChat(req, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.wantOptions, got.StreamOptions)
		})
	}
}

func TestClaudeMessagesRequestToOpenAIChatBasics(t *testing.T) {
	streamTrue := true
	req := dto.ClaudeRequest{
		Model:     "deepseek-v4-flash",
		MaxTokens: loPtr(uint(256)),
		Stream:    &streamTrue,
		System:    "system rules",
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "hello"},
		},
	}

	got, err := ClaudeMessagesRequestToOpenAIChat(req, nil)
	require.NoError(t, err)
	assert.Equal(t, "deepseek-v4-flash", got.Model)
	require.Len(t, got.Messages, 2)
	assert.Equal(t, "system", got.Messages[0].Role)
	assert.Equal(t, "user", got.Messages[1].Role)
	assert.True(t, *got.Stream)
	require.NotNil(t, got.StreamOptions)
	assert.True(t, got.StreamOptions.IncludeUsage)
}

func loPtr[T any](v T) *T {
	return &v
}
