package helper

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withInputModalityPolicy(t *testing.T, policy model_setting.InputModalityPolicy) {
	t.Helper()
	original := *model_setting.GetGlobalSettings()
	t.Cleanup(func() {
		*model_setting.GetGlobalSettings() = original
	})
	model_setting.GetGlobalSettings().InputModalityPolicy = policy
}

func newJSONContext(t *testing.T, path, body string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c
}

func requireUnsupportedImage(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Equal(t, types.ErrorCodeUnsupportedInputType, apiErr.GetErrorCode())
	assert.Contains(t, err.Error(), "unsupported input type: image")
	openAIErr := apiErr.ToOpenAIError()
	assert.Equal(t, "invalid_request_error", openAIErr.Type)
	assert.Equal(t, string(types.ErrorCodeUnsupportedInputType), openAIErr.Code)
}

func TestValidateInputModalitiesRejectsLatestTurnImage(t *testing.T) {
	withInputModalityPolicy(t, model_setting.InputModalityPolicy{
		Enabled: true,
		Models: map[string]model_setting.InputModalities{
			"deepseek-v4-flash": {Image: common.GetPointer(false)},
		},
	})

	t.Run("responses nested input_image", func(t *testing.T) {
		body := `{
			"model":"deepseek-v4-flash",
			"input":[
				{"role":"user","content":[
					{"type":"input_text","text":"describe this"},
					{"type":"input_image","image_url":"data:image/png;base64,abc","detail":"low"}
				]}
			]
		}`
		c := newJSONContext(t, "/v1/responses", body)
		_, err := GetAndValidateRequest(c, types.RelayFormatOpenAIResponses)
		requireUnsupportedImage(t, err)
	})

	t.Run("responses top-level input_image", func(t *testing.T) {
		body := `{
			"model":"deepseek-v4-flash",
			"input":[
				{"type":"input_text","text":"describe this"},
				{"type":"input_image","image_url":"https://example.test/a.png"}
			]
		}`
		c := newJSONContext(t, "/v1/responses", body)
		_, err := GetAndValidateRequest(c, types.RelayFormatOpenAIResponses)
		requireUnsupportedImage(t, err)
	})

	t.Run("chat completions image_url", func(t *testing.T) {
		body := `{
			"model":"deepseek-v4-flash-max",
			"messages":[{"role":"user","content":[
				{"type":"text","text":"describe this"},
				{"type":"image_url","image_url":{"url":"data:image/png;base64,abc","detail":"low"}}
			]}]
		}`
		c := newJSONContext(t, "/v1/chat/completions", body)
		_, err := GetAndValidateRequest(c, types.RelayFormatOpenAI)
		requireUnsupportedImage(t, err)
	})

	t.Run("original model from context", func(t *testing.T) {
		body := `{
			"model":"alias-model",
			"input":[{"type":"input_image","image_url":"https://example.test/a.png"}]
		}`
		c := newJSONContext(t, "/v1/responses", body)
		common.SetContextKey(c, constant.ContextKeyOriginalModel, "deepseek-v4-flash")
		_, err := GetAndValidateRequest(c, types.RelayFormatOpenAIResponses)
		requireUnsupportedImage(t, err)
	})
}

func TestValidateInputModalitiesDropsHistoricalImageTurns(t *testing.T) {
	withInputModalityPolicy(t, model_setting.InputModalityPolicy{
		Enabled: true,
		Models: map[string]model_setting.InputModalities{
			"deepseek-v4-flash": {Image: common.GetPointer(false)},
		},
	})

	t.Run("responses history with image then text-only turn", func(t *testing.T) {
		body := `{
			"model":"deepseek-v4-flash",
			"input":[
				{"role":"user","content":[
					{"type":"input_text","text":"解释图片"},
					{"type":"input_image","image_url":"https://example.test/a.png"}
				]},
				{"role":"user","content":[
					{"type":"input_text","text":"你好"}
				]}
			]
		}`
		c := newJSONContext(t, "/v1/responses", body)
		req, err := GetAndValidateRequest(c, types.RelayFormatOpenAIResponses)
		require.NoError(t, err)
		responsesReq, ok := req.(*dto.OpenAIResponsesRequest)
		require.True(t, ok)

		var items []map[string]any
		require.NoError(t, common.Unmarshal(responsesReq.Input, &items))
		require.Len(t, items, 1)
		assert.Equal(t, "user", items[0]["role"])
		assert.False(t, jsonHasTypeField(responsesReq.Input, "input_image"))

		content, err := common.Marshal(items[0]["content"])
		require.NoError(t, err)
		assert.Contains(t, string(content), "你好")
		assert.NotContains(t, string(content), "解释图片")
	})

	t.Run("chat history with image then text-only turn", func(t *testing.T) {
		body := `{
			"model":"deepseek-v4-flash",
			"messages":[
				{"role":"user","content":[
					{"type":"text","text":"解释图片"},
					{"type":"image_url","image_url":{"url":"https://example.test/a.png"}}
				]},
				{"role":"user","content":"你好"}
			]
		}`
		c := newJSONContext(t, "/v1/chat/completions", body)
		req, err := GetAndValidateRequest(c, types.RelayFormatOpenAI)
		require.NoError(t, err)
		chatReq, ok := req.(*dto.GeneralOpenAIRequest)
		require.True(t, ok)
		require.Len(t, chatReq.Messages, 1)
		assert.Equal(t, "你好", chatReq.Messages[0].StringContent())
	})

	t.Run("responses flat history image then trailing text", func(t *testing.T) {
		body := `{
			"model":"deepseek-v4-flash",
			"input":[
				{"type":"input_text","text":"解释图片"},
				{"type":"input_image","image_url":"https://example.test/a.png"},
				{"type":"input_text","text":"你好"}
			]
		}`
		c := newJSONContext(t, "/v1/responses", body)
		req, err := GetAndValidateRequest(c, types.RelayFormatOpenAIResponses)
		require.NoError(t, err)
		responsesReq, ok := req.(*dto.OpenAIResponsesRequest)
		require.True(t, ok)

		var items []map[string]any
		require.NoError(t, common.Unmarshal(responsesReq.Input, &items))
		require.Len(t, items, 1)
		assert.Equal(t, "input_text", items[0]["type"])
		assert.Equal(t, "你好", items[0]["text"])
		assert.False(t, jsonHasTypeField(responsesReq.Input, "input_image"))
	})

	t.Run("responses typed message history then text-only turn", func(t *testing.T) {
		body := `{
			"model":"deepseek-v4-flash",
			"input":[
				{"type":"message","role":"user","content":[
					{"type":"input_text","text":"解释图片"},
					{"type":"input_image","image_url":"https://example.test/a.png"}
				]},
				{"type":"message","role":"user","content":[
					{"type":"input_text","text":"你好"}
				]}
			]
		}`
		c := newJSONContext(t, "/v1/responses", body)
		req, err := GetAndValidateRequest(c, types.RelayFormatOpenAIResponses)
		require.NoError(t, err)
		responsesReq, ok := req.(*dto.OpenAIResponsesRequest)
		require.True(t, ok)

		var items []map[string]any
		require.NoError(t, common.Unmarshal(responsesReq.Input, &items))
		require.Len(t, items, 1)
		assert.Equal(t, "user", items[0]["role"])
		assert.False(t, jsonHasTypeField(responsesReq.Input, "input_image"))
		content, err := common.Marshal(items[0]["content"])
		require.NoError(t, err)
		assert.Contains(t, string(content), "你好")
		assert.NotContains(t, string(content), "解释图片")
	})

	t.Run("responses drops assistant after historical image turn", func(t *testing.T) {
		body := `{
			"model":"deepseek-v4-flash",
			"input":[
				{"role":"user","content":[
					{"type":"input_text","text":"解释图片"},
					{"type":"input_image","image_url":"https://example.test/a.png"}
				]},
				{"role":"assistant","content":[{"type":"output_text","text":"unsupported input type: image"}]},
				{"role":"user","content":[{"type":"input_text","text":"你好"}]}
			]
		}`
		c := newJSONContext(t, "/v1/responses", body)
		req, err := GetAndValidateRequest(c, types.RelayFormatOpenAIResponses)
		require.NoError(t, err)
		responsesReq, ok := req.(*dto.OpenAIResponsesRequest)
		require.True(t, ok)

		var items []map[string]any
		require.NoError(t, common.Unmarshal(responsesReq.Input, &items))
		require.Len(t, items, 1)
		assert.Equal(t, "user", items[0]["role"])
		content, err := common.Marshal(items[0]["content"])
		require.NoError(t, err)
		assert.Contains(t, string(content), "你好")
	})

	t.Run("responses drops function calls after historical image turn", func(t *testing.T) {
		body := `{
			"model":"deepseek-v4-flash",
			"input":[
				{"type":"message","role":"user","content":[
					{"type":"input_text","text":"解释图片"},
					{"type":"input_image","image_url":"https://example.test/a.png"}
				]},
				{"type":"function_call","name":"read_file","call_id":"c1","arguments":"{}"},
				{"type":"function_call_output","call_id":"c1","output":"ok"},
				{"type":"message","role":"user","content":[{"type":"input_text","text":"你好"}]}
			]
		}`
		c := newJSONContext(t, "/v1/responses", body)
		req, err := GetAndValidateRequest(c, types.RelayFormatOpenAIResponses)
		require.NoError(t, err)
		responsesReq, ok := req.(*dto.OpenAIResponsesRequest)
		require.True(t, ok)

		var items []map[string]any
		require.NoError(t, common.Unmarshal(responsesReq.Input, &items))
		require.Len(t, items, 1)
		assert.Equal(t, "message", items[0]["type"])
		assert.Equal(t, "user", items[0]["role"])
		assert.False(t, jsonHasTypeField(responsesReq.Input, "function_call"))
	})

	t.Run("chat drops assistant after historical image turn", func(t *testing.T) {
		body := `{
			"model":"deepseek-v4-flash",
			"messages":[
				{"role":"user","content":[
					{"type":"text","text":"解释图片"},
					{"type":"image_url","image_url":{"url":"https://example.test/a.png"}}
				]},
				{"role":"assistant","content":"unsupported input type: image"},
				{"role":"user","content":"你好"}
			]
		}`
		c := newJSONContext(t, "/v1/chat/completions", body)
		req, err := GetAndValidateRequest(c, types.RelayFormatOpenAI)
		require.NoError(t, err)
		chatReq, ok := req.(*dto.GeneralOpenAIRequest)
		require.True(t, ok)
		require.Len(t, chatReq.Messages, 1)
		assert.Equal(t, "你好", chatReq.Messages[0].StringContent())
	})
}

func TestValidateInputModalitiesAllowsCompatibleRequests(t *testing.T) {
	withInputModalityPolicy(t, model_setting.InputModalityPolicy{
		Enabled: true,
		Models: map[string]model_setting.InputModalities{
			"deepseek-v4-flash": {Image: common.GetPointer(false)},
		},
	})

	t.Run("text-only responses", func(t *testing.T) {
		c := newJSONContext(t, "/v1/responses", `{"model":"deepseek-v4-flash","input":"hello"}`)
		req, err := GetAndValidateRequest(c, types.RelayFormatOpenAIResponses)
		require.NoError(t, err)
		require.NotNil(t, req)
	})

	t.Run("vision model with image is unchanged", func(t *testing.T) {
		body := `{
			"model":"gpt-4o",
			"messages":[{"role":"user","content":[
				{"type":"text","text":"describe this"},
				{"type":"image_url","image_url":{"url":"https://example.test/a.png"}}
			]}]
		}`
		c := newJSONContext(t, "/v1/chat/completions", body)
		req, err := GetAndValidateRequest(c, types.RelayFormatOpenAI)
		require.NoError(t, err)
		require.NotNil(t, req)
	})

	t.Run("disabled policy forwards image", func(t *testing.T) {
		model_setting.GetGlobalSettings().InputModalityPolicy.Enabled = false
		body := `{
			"model":"deepseek-v4-flash",
			"input":[{"type":"input_image","image_url":"https://example.test/a.png"}]
		}`
		c := newJSONContext(t, "/v1/responses", body)
		req, err := GetAndValidateRequest(c, types.RelayFormatOpenAIResponses)
		require.NoError(t, err)
		require.NotNil(t, req)
	})
}

func TestValidateInputModalitiesClaudeAndGemini(t *testing.T) {
	withInputModalityPolicy(t, model_setting.InputModalityPolicy{
		Enabled: true,
		Models: map[string]model_setting.InputModalities{
			"text-only": {Image: common.GetPointer(false)},
		},
	})

	t.Run("claude latest turn image", func(t *testing.T) {
		body := `{
			"model":"text-only",
			"max_tokens":16,
			"messages":[{"role":"user","content":[
				{"type":"text","text":"describe this"},
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"abc"}}
			]}]
		}`
		c := newJSONContext(t, "/v1/messages", body)
		_, err := GetAndValidateRequest(c, types.RelayFormatClaude)
		requireUnsupportedImage(t, err)
	})

	t.Run("claude drops historical image turn", func(t *testing.T) {
		body := `{
			"model":"text-only",
			"max_tokens":16,
			"messages":[
				{"role":"user","content":[
					{"type":"text","text":"解释图片"},
					{"type":"image","source":{"type":"base64","media_type":"image/png","data":"abc"}}
				]},
				{"role":"assistant","content":"unsupported input type: image"},
				{"role":"user","content":"你好"}
			]
		}`
		c := newJSONContext(t, "/v1/messages", body)
		req, err := GetAndValidateRequest(c, types.RelayFormatClaude)
		require.NoError(t, err)
		claudeReq, ok := req.(*dto.ClaudeRequest)
		require.True(t, ok)
		require.Len(t, claudeReq.Messages, 1)
		assert.Equal(t, "你好", claudeReq.Messages[0].GetStringContent())
	})

	t.Run("claude strips system image and forwards text turn", func(t *testing.T) {
		body := `{
			"model":"text-only",
			"max_tokens":16,
			"system":[
				{"type":"text","text":"you are a helper"},
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"abc"}}
			],
			"messages":[{"role":"user","content":"你好"}]
		}`
		c := newJSONContext(t, "/v1/messages", body)
		req, err := GetAndValidateRequest(c, types.RelayFormatClaude)
		require.NoError(t, err)
		claudeReq, ok := req.(*dto.ClaudeRequest)
		require.True(t, ok)
		require.Len(t, claudeReq.Messages, 1)
		system := claudeReq.ParseSystem()
		require.Len(t, system, 1)
		assert.Equal(t, "text", system[0].Type)
	})

	t.Run("gemini inline image uses original model", func(t *testing.T) {
		body := `{
			"contents":[{"parts":[
				{"text":"describe this"},
				{"inline_data":{"mime_type":"image/png","data":"abc"}}
			]}]
		}`
		c := newJSONContext(t, "/v1beta/models/text-only:generateContent", body)
		common.SetContextKey(c, constant.ContextKeyOriginalModel, "text-only")
		_, err := GetAndValidateRequest(c, types.RelayFormatGemini)
		requireUnsupportedImage(t, err)
	})

	t.Run("gemini drops historical image turn", func(t *testing.T) {
		body := `{
			"contents":[
				{"role":"user","parts":[
					{"text":"解释图片"},
					{"inline_data":{"mime_type":"image/png","data":"abc"}}
				]},
				{"role":"model","parts":[{"text":"unsupported input type: image"}]},
				{"role":"user","parts":[{"text":"你好"}]}
			]
		}`
		c := newJSONContext(t, "/v1beta/models/text-only:generateContent", body)
		common.SetContextKey(c, constant.ContextKeyOriginalModel, "text-only")
		req, err := GetAndValidateRequest(c, types.RelayFormatGemini)
		require.NoError(t, err)
		geminiReq, ok := req.(*dto.GeminiChatRequest)
		require.True(t, ok)
		require.Len(t, geminiReq.Contents, 1)
		require.Len(t, geminiReq.Contents[0].Parts, 1)
		assert.Equal(t, "你好", geminiReq.Contents[0].Parts[0].Text)
	})

	t.Run("gemini drops empty-role historical image before user text", func(t *testing.T) {
		body := `{
			"contents":[
				{"parts":[
					{"text":"解释图片"},
					{"inline_data":{"mime_type":"image/png","data":"abc"}}
				]},
				{"role":"user","parts":[{"text":"你好"}]}
			]
		}`
		c := newJSONContext(t, "/v1beta/models/text-only:generateContent", body)
		common.SetContextKey(c, constant.ContextKeyOriginalModel, "text-only")
		req, err := GetAndValidateRequest(c, types.RelayFormatGemini)
		require.NoError(t, err)
		geminiReq, ok := req.(*dto.GeminiChatRequest)
		require.True(t, ok)
		require.Len(t, geminiReq.Contents, 1)
		assert.Equal(t, "你好", geminiReq.Contents[0].Parts[0].Text)
	})

	t.Run("gemini last model role does not hide previous user turn", func(t *testing.T) {
		body := `{
			"contents":[
				{"role":"user","parts":[
					{"text":"解释图片"},
					{"inline_data":{"mime_type":"image/png","data":"abc"}}
				]},
				{"role":"user","parts":[{"text":"你好"}]},
				{"role":"model","parts":[{"text":"ok"}]}
			]
		}`
		c := newJSONContext(t, "/v1beta/models/text-only:generateContent", body)
		common.SetContextKey(c, constant.ContextKeyOriginalModel, "text-only")
		req, err := GetAndValidateRequest(c, types.RelayFormatGemini)
		require.NoError(t, err)
		geminiReq, ok := req.(*dto.GeminiChatRequest)
		require.True(t, ok)
		require.Len(t, geminiReq.Contents, 2)
		assert.Equal(t, "user", geminiReq.Contents[0].Role)
		assert.Equal(t, "你好", geminiReq.Contents[0].Parts[0].Text)
		assert.Equal(t, "model", geminiReq.Contents[1].Role)
	})
}

func TestDefaultPolicyRejectsDeepSeekV4FlashImage(t *testing.T) {
	body := `{
		"model":"deepseek-v4-flash",
		"input":[{"type":"input_image","image_url":"https://example.test/a.png"}]
	}`
	c := newJSONContext(t, "/v1/responses", body)
	_, err := GetAndValidateRequest(c, types.RelayFormatOpenAIResponses)
	requireUnsupportedImage(t, err)
}

func TestCollectResponsesTopLevelImage(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{
		Model: "deepseek-v4-flash",
		Input: json.RawMessage(`[{"type":"input_image","image_url":"https://example.test/a.png"}]`),
	}
	present := collectRequestInputTypes(request)
	assert.True(t, present[types.FileTypeImage])
}

func TestGetAndValidateTextRequestStillBoundsMaxTokens(t *testing.T) {
	c := newJSONContext(t, "/v1/chat/completions", `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"max_tokens":1}`)
	req, err := GetAndValidateTextRequest(c, relayconstant.RelayModeChatCompletions)
	require.NoError(t, err)
	require.EqualValues(t, 1, *req.MaxTokens)
}

func jsonHasTypeField(data []byte, typeName string) bool {
	var value any
	if err := common.Unmarshal(data, &value); err != nil {
		return false
	}
	return walkJSONHasType(value, typeName)
}

func walkJSONHasType(value any, typeName string) bool {
	switch item := value.(type) {
	case map[string]any:
		if got, ok := item["type"].(string); ok && got == typeName {
			return true
		}
		for _, child := range item {
			if walkJSONHasType(child, typeName) {
				return true
			}
		}
	case []any:
		for _, child := range item {
			if walkJSONHasType(child, typeName) {
				return true
			}
		}
	}
	return false
}
