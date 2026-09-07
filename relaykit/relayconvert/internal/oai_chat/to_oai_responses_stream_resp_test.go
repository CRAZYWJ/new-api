package oaichat

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/require"
)

func TestChatToResponsesDoneEventsCarryFullContent(t *testing.T) {
	state := NewChatToResponsesStreamState("resp_test_1", "deepseek-v4-flash")

	var all []ChatToResponsesStreamEvent
	add := func(chunk *dto.ChatCompletionsStreamResponse) {
		events, err := ChatCompletionsStreamChunkToResponsesEvents(chunk, state)
		require.NoError(t, err)
		all = append(all, events...)
	}

	add(&dto.ChatCompletionsStreamResponse{
		Id: "chatcmpl-1", Model: "deepseek-v4-flash", Created: 1700000000,
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: ptr("let me check")},
		}},
	})

	i0 := 0
	add(&dto.ChatCompletionsStreamResponse{
		Id: "chatcmpl-1",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				ToolCalls: []dto.ToolCallResponse{{
					Index:    ptr(i0),
					ID:       "call_abc",
					Type:     "function",
					Function: dto.FunctionResponse{Name: "bash", Arguments: `{"co`},
				}},
			},
		}},
	})
	add(&dto.ChatCompletionsStreamResponse{
		Id: "chatcmpl-1",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				ToolCalls: []dto.ToolCallResponse{{
					Index:    ptr(i0),
					Function: dto.FunctionResponse{Arguments: `mmand":"ls"}`},
				}},
			},
		}},
	})
	add(&dto.ChatCompletionsStreamResponse{
		Id: "chatcmpl-1",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			FinishReason: ptr("tool_calls"),
		}},
	})

	all = append(all, FinalizeChatCompletionsStreamToResponses(state)...)

	var argsDone, textDone *dto.ResponsesStreamResponse
	for i := range all {
		switch all[i].Type {
		case "response.function_call_arguments.done":
			argsDone = &all[i].Payload
		case "response.output_text.done":
			textDone = &all[i].Payload
		}
	}

	require.NotNil(t, argsDone, "function_call_arguments.done event missing")
	require.Equal(t, `{"command":"ls"}`, argsDone.Arguments)
	require.Equal(t, "call_abc", argsDone.ItemID)

	require.NotNil(t, textDone, "output_text.done event missing")
	require.Equal(t, "let me check", textDone.Text)

	b, err := kitutil.Marshal(argsDone)
	require.NoError(t, err)
	require.Contains(t, string(b), `"arguments":"{\"command\":\"ls\"}"`)

	b2, err := kitutil.Marshal(textDone)
	require.NoError(t, err)
	require.Contains(t, string(b2), `"text":"let me check"`)
}
