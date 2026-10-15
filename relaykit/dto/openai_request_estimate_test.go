package dto

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesRequestGetTokenCountMetaCountsToolItems(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Model: "deepseek-v4-flash",
		Input: []byte(`[
			{"role":"user","content":[{"type":"input_text","text":"check the disk"}]},
			{"role":"assistant","content":[{"type":"output_text","text":"let me check"}]},
			{"type":"function_call","call_id":"c1","name":"bash","arguments":"{\"command\":\"df -h\"}"},
			{"type":"function_call_output","call_id":"c1","output":"lots of tool output"},
			{"type":"custom_tool_call","call_id":"c2","name":"patch","input":"diff body"},
			{"type":"custom_tool_call_output","call_id":"c2","output":"applied"}
		]`),
	}

	meta := req.GetTokenCountMeta()
	combined := meta.CombineText
	for _, want := range []string{
		"check the disk",
		"let me check",
		`{"command":"df -h"}`,
		"lots of tool output",
		"diff body",
		"applied",
	} {
		assert.Contains(t, combined, want)
	}
}

func TestResponsesRequestParseInputArrayToolOutput(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: []byte(`[
			{"type":"function_call","call_id":"c1","name":"read","arguments":"{\"path\":\"/tmp/a\"}"},
			{"type":"function_call_output","call_id":"c1","output":[{"type":"input_text","text":"file line"}]}
		]`),
	}

	inputs := req.ParseInput()
	require.NotEmpty(t, inputs)
	// rc.44 normalizes tool-call items to input_text: the arguments string
	// and the text inside an array-typed output both land as plain text.
	var foundArgs, foundArrayOutput bool
	for _, in := range inputs {
		if in.Type != "input_text" {
			continue
		}
		if strings.Contains(in.Text, `{"path":"/tmp/a"}`) {
			foundArgs = true
		}
		if strings.Contains(in.Text, "file line") {
			foundArrayOutput = true
		}
	}
	assert.True(t, foundArgs, "function_call arguments missing from ParseInput")
	assert.True(t, foundArrayOutput, "array-typed function_call_output missing from ParseInput")
}
