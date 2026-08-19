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
	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
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

func TestValidateInputModalitiesRejectsImageForTextOnlyModel(t *testing.T) {
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

	t.Run("claude image", func(t *testing.T) {
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
