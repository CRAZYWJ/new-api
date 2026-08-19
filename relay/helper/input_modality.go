package helper

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func ValidateInputModalities(c *gin.Context, request dto.Request) error {
	if request == nil {
		return nil
	}
	policy := model_setting.GetInputModalityPolicy()
	if !policy.Enabled || len(policy.Models) == 0 {
		return nil
	}

	modelName := requestModelName(c, request)
	caps, ok := model_setting.LookupInputModalities(modelName)
	if !ok {
		return nil
	}

	present := collectRequestInputTypes(request)
	if caps.Image != nil && !*caps.Image && present[types.FileTypeImage] {
		return unsupportedInputTypeError("image")
	}
	if caps.Audio != nil && !*caps.Audio && present[types.FileTypeAudio] {
		return unsupportedInputTypeError("audio")
	}
	if caps.File != nil && !*caps.File && present[types.FileTypeFile] {
		return unsupportedInputTypeError("file")
	}
	return nil
}

func unsupportedInputTypeError(inputType string) error {
	return types.WithOpenAIError(types.OpenAIError{
		Message: fmt.Sprintf("unsupported input type: %s", inputType),
		Type:    "invalid_request_error",
		Code:    string(types.ErrorCodeUnsupportedInputType),
	}, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

func requestModelName(c *gin.Context, request dto.Request) string {
	if c != nil {
		if name := common.GetContextKeyString(c, constant.ContextKeyOriginalModel); name != "" {
			return name
		}
	}
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		return r.Model
	case *dto.OpenAIResponsesRequest:
		return r.Model
	case *dto.ClaudeRequest:
		return r.Model
	case *dto.EmbeddingRequest:
		return r.Model
	case *dto.AudioRequest:
		return r.Model
	default:
		return ""
	}
}

func collectRequestInputTypes(request dto.Request) map[types.FileType]bool {
	present := make(map[types.FileType]bool)
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		collectChatInputTypes(r, present)
	case *dto.OpenAIResponsesRequest:
		collectResponsesInputTypes(r, present)
	case *dto.ClaudeRequest:
		collectClaudeInputTypes(r, present)
	case *dto.GeminiChatRequest:
		collectGeminiInputTypes(r, present)
	}
	return present
}

func collectChatInputTypes(request *dto.GeneralOpenAIRequest, present map[types.FileType]bool) {
	if request == nil {
		return
	}
	for _, message := range request.Messages {
		for _, part := range message.ParseContent() {
			switch part.Type {
			case dto.ContentTypeImageURL:
				present[types.FileTypeImage] = true
			case dto.ContentTypeInputAudio:
				present[types.FileTypeAudio] = true
			case dto.ContentTypeFile:
				present[types.FileTypeFile] = true
			case dto.ContentTypeVideoUrl:
				present[types.FileTypeVideo] = true
			}
		}
	}
}

func collectResponsesInputTypes(request *dto.OpenAIResponsesRequest, present map[types.FileType]bool) {
	if request == nil {
		return
	}
	for _, input := range request.ParseInput() {
		switch input.Type {
		case "input_image":
			present[types.FileTypeImage] = true
		case "input_file":
			present[types.FileTypeFile] = true
		}
	}
	if jsonHasTypeField(request.Input, "input_image") {
		present[types.FileTypeImage] = true
	}
	if jsonHasTypeField(request.Input, "input_file") {
		present[types.FileTypeFile] = true
	}
}

func collectClaudeInputTypes(request *dto.ClaudeRequest, present map[types.FileType]bool) {
	if request == nil {
		return
	}
	if !request.IsStringSystem() {
		for _, media := range request.ParseSystem() {
			if media.Type == "image" {
				present[types.FileTypeImage] = true
			}
		}
	}
	for _, message := range request.Messages {
		if message.IsStringContent() {
			continue
		}
		content, _ := message.ParseContent()
		for _, media := range content {
			if media.Type == "image" {
				present[types.FileTypeImage] = true
			}
		}
	}
}

func collectGeminiInputTypes(request *dto.GeminiChatRequest, present map[types.FileType]bool) {
	if request == nil {
		return
	}
	markGeminiParts := func(parts []dto.GeminiPart) {
		for _, part := range parts {
			if part.InlineData != nil && strings.HasPrefix(part.InlineData.MimeType, "image/") {
				present[types.FileTypeImage] = true
			}
			if part.InlineData != nil && strings.HasPrefix(part.InlineData.MimeType, "audio/") {
				present[types.FileTypeAudio] = true
			}
			if part.FileData != nil && strings.HasPrefix(part.FileData.MimeType, "image/") {
				present[types.FileTypeImage] = true
			}
			if part.FileData != nil && strings.HasPrefix(part.FileData.MimeType, "audio/") {
				present[types.FileTypeAudio] = true
			}
		}
	}
	for _, content := range request.Contents {
		markGeminiParts(content.Parts)
	}
	if request.SystemInstructions != nil {
		markGeminiParts(request.SystemInstructions.Parts)
	}
}

func jsonHasTypeField(data []byte, typeName string) bool {
	if len(data) == 0 || typeName == "" {
		return false
	}
	var value any
	if err := common.Unmarshal(data, &value); err != nil {
		return false
	}
	return walkJSONTypeField(value, typeName)
}

func walkJSONTypeField(value any, typeName string) bool {
	switch item := value.(type) {
	case map[string]any:
		if got, ok := item["type"].(string); ok && got == typeName {
			return true
		}
		for _, child := range item {
			if walkJSONTypeField(child, typeName) {
				return true
			}
		}
	case []any:
		for _, child := range item {
			if walkJSONTypeField(child, typeName) {
				return true
			}
		}
	}
	return false
}
