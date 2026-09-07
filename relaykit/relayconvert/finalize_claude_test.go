package relayconvert

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFinalizeChatStreamResponseToClaudeEmitsTerminalEvents(t *testing.T) {
	info := &convmeta.Values{}
	state := info.EnsureClaudeConvertInfo()
	state.LastMessagesType = convmeta.LastMessageTypeText
	state.Index = 0
	state.FinishReason = "stop"
	state.Usage = &dto.Usage{PromptTokens: 12, CompletionTokens: 34}

	responses, usage, err := FinalizeChatStreamResponseToClaude(nil, info)
	require.NoError(t, err)
	assert.Same(t, state.Usage, usage)

	require.Len(t, responses, 3)
	assert.Equal(t, "content_block_stop", responses[0].Type)
	require.NotNil(t, responses[0].Index)
	assert.Equal(t, 0, *responses[0].Index)
	assert.Equal(t, "message_delta", responses[1].Type)
	require.NotNil(t, responses[1].Delta)
	require.NotNil(t, responses[1].Delta.StopReason)
	assert.Equal(t, "end_turn", *responses[1].Delta.StopReason)
	assert.Equal(t, "message_stop", responses[2].Type)
	assert.True(t, info.ClaudeConvertInfo.Done)

	responsesAgain, _, err := FinalizeChatStreamResponseToClaude(nil, info)
	require.NoError(t, err)
	assert.Empty(t, responsesAgain)
}

func TestFinalizeChatStreamResponseToClaudeToolsBlock(t *testing.T) {
	info := &convmeta.Values{}
	state := info.EnsureClaudeConvertInfo()
	state.LastMessagesType = convmeta.LastMessageTypeTools
	state.ToolCallBaseIndex = 1
	state.ToolCallMaxIndexOffset = 1
	state.FinishReason = "tool_calls"

	responses, _, err := FinalizeChatStreamResponseToClaude(nil, info)
	require.NoError(t, err)

	var stopIndexes []int
	for _, resp := range responses {
		if resp.Type == "content_block_stop" {
			require.NotNil(t, resp.Index)
			stopIndexes = append(stopIndexes, *resp.Index)
		}
	}
	assert.Equal(t, []int{1, 2}, stopIndexes)
	assert.Equal(t, "message_stop", responses[len(responses)-1].Type)
}

func TestFinalizeChatStreamResponseToClaudeNoopWhenAlreadyDone(t *testing.T) {
	info := &convmeta.Values{}
	info.EnsureClaudeConvertInfo().Done = true

	responses, _, err := FinalizeChatStreamResponseToClaude(nil, info)
	require.NoError(t, err)
	assert.Empty(t, responses)
}

func TestFinalizeChatStreamResponseToClaudeNilInfo(t *testing.T) {
	responses, _, err := FinalizeChatStreamResponseToClaude(nil, nil)
	require.NoError(t, err)
	require.Len(t, responses, 2)
	assert.Equal(t, "message_delta", responses[0].Type)
	assert.Equal(t, "message_stop", responses[1].Type)
}
