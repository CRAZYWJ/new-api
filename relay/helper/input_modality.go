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

// ValidateInputModalities enforces inbound modality policy:
//   - if the latest user turn contains a disabled modality → HTTP 400
//   - otherwise drop entire historical turns that contain disabled modalities
//     (so clients like Codex can continue after a failed image upload)
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

	disabled := disabledInputTypes(caps)
	if len(disabled) == 0 {
		return nil
	}

	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		return applyChatInputModalityPolicy(r, disabled)
	case *dto.OpenAIResponsesRequest:
		return applyResponsesInputModalityPolicy(r, disabled)
	case *dto.ClaudeRequest:
		return applyClaudeInputModalityPolicy(r, disabled)
	case *dto.GeminiChatRequest:
		return applyGeminiInputModalityPolicy(r, disabled)
	default:
		return nil
	}
}

func disabledInputTypes(caps model_setting.InputModalities) map[types.FileType]bool {
	disabled := make(map[types.FileType]bool)
	if caps.Image != nil && !*caps.Image {
		disabled[types.FileTypeImage] = true
	}
	if caps.Audio != nil && !*caps.Audio {
		disabled[types.FileTypeAudio] = true
	}
	if caps.File != nil && !*caps.File {
		disabled[types.FileTypeFile] = true
	}
	return disabled
}

func unsupportedInputTypeError(inputType string) error {
	return types.WithOpenAIError(types.OpenAIError{
		Message: fmt.Sprintf("unsupported input type: %s", inputType),
		Type:    "invalid_request_error",
		Code:    string(types.ErrorCodeUnsupportedInputType),
	}, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

func invalidModalityInspectError() error {
	return types.WithOpenAIError(types.OpenAIError{
		Message: "failed to parse request input for modality validation",
		Type:    "invalid_request_error",
		Code:    string(types.ErrorCodeBadRequestBody),
	}, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

func firstDisabledType(present map[types.FileType]bool, disabled map[types.FileType]bool) string {
	if disabled[types.FileTypeImage] && present[types.FileTypeImage] {
		return "image"
	}
	if disabled[types.FileTypeAudio] && present[types.FileTypeAudio] {
		return "audio"
	}
	if disabled[types.FileTypeFile] && present[types.FileTypeFile] {
		return "file"
	}
	return ""
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

func applyChatInputModalityPolicy(request *dto.GeneralOpenAIRequest, disabled map[types.FileType]bool) error {
	if request == nil || len(request.Messages) == 0 {
		return nil
	}

	latestIdx := -1
	for i := len(request.Messages) - 1; i >= 0; i-- {
		if strings.EqualFold(request.Messages[i].Role, "user") {
			latestIdx = i
			break
		}
	}
	if latestIdx < 0 {
		return nil
	}

	if name := firstDisabledType(chatMessageInputTypes(request.Messages[latestIdx]), disabled); name != "" {
		return unsupportedInputTypeError(name)
	}

	drop := make([]bool, len(request.Messages))
	for i := 0; i < latestIdx; {
		if firstDisabledType(chatMessageInputTypes(request.Messages[i]), disabled) == "" {
			i++
			continue
		}
		right := i + 1
		for right < latestIdx && !strings.EqualFold(request.Messages[right].Role, "user") {
			right++
		}
		for j := i; j < right; j++ {
			drop[j] = true
		}
		i = right
	}

	kept := make([]dto.Message, 0, len(request.Messages))
	for i, message := range request.Messages {
		if drop[i] {
			continue
		}
		kept = append(kept, message)
	}
	request.Messages = kept
	return nil
}

func chatMessageInputTypes(message dto.Message) map[types.FileType]bool {
	present := make(map[types.FileType]bool)
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
	return present
}

func applyResponsesInputModalityPolicy(request *dto.OpenAIResponsesRequest, disabled map[types.FileType]bool) error {
	if request == nil || len(request.Input) == 0 {
		return nil
	}
	if common.GetJsonType(request.Input) == "string" {
		return nil
	}
	if common.GetJsonType(request.Input) != "array" {
		return nil
	}

	var items []any
	if err := common.Unmarshal(request.Input, &items); err != nil {
		return invalidModalityInspectError()
	}
	if len(items) == 0 {
		return nil
	}

	latestStart := findResponsesLatestTurnStart(items)
	if latestStart < 0 {
		return nil
	}

	for i := latestStart; i < len(items); i++ {
		if name := firstDisabledType(responsesItemInputTypes(items[i]), disabled); name != "" {
			return unsupportedInputTypeError(name)
		}
	}

	drop := make([]bool, len(items))
	for i := 0; i < latestStart; {
		if firstDisabledType(responsesItemInputTypes(items[i]), disabled) == "" {
			i++
			continue
		}
		left, right := expandResponsesUnsupportedTurn(items, i, latestStart)
		for j := left; j < right; j++ {
			drop[j] = true
		}
		i = right
	}

	kept := make([]any, 0, len(items))
	for i, item := range items {
		if drop[i] {
			continue
		}
		kept = append(kept, item)
	}
	encoded, err := common.Marshal(kept)
	if err != nil {
		return invalidModalityInspectError()
	}
	request.Input = encoded
	return nil
}

func findResponsesLatestTurnStart(items []any) int {
	for i := len(items) - 1; i >= 0; i-- {
		if isResponsesUserTurnStart(items[i]) {
			return i
		}
	}
	return -1
}

func expandResponsesUnsupportedTurn(items []any, idx, latestStart int) (int, int) {
	left := idx
	if isResponsesFlatUserPart(items[idx]) {
		for left > 0 && isResponsesFlatUserPart(items[left-1]) {
			left--
		}
	}
	right := idx + 1
	if isResponsesFlatUserPart(items[idx]) {
		for right < latestStart && isResponsesFlatUserPart(items[right]) {
			right++
		}
	}
	for right < latestStart && !isResponsesUserTurnStart(items[right]) {
		right++
	}
	return left, right
}

func isResponsesUserTurnStart(item any) bool {
	role := responsesItemRole(item)
	if role == "user" {
		return true
	}
	if role == "assistant" || role == "system" {
		return false
	}
	switch responsesItemType(item) {
	case "message", "input_text", "input_image", "input_file":
		return true
	default:
		return false
	}
}

func isResponsesFlatUserPart(item any) bool {
	switch responsesItemType(item) {
	case "input_text", "input_image", "input_file":
		return true
	default:
		return false
	}
}

func responsesItemRole(item any) string {
	obj, ok := item.(map[string]any)
	if !ok {
		return ""
	}
	role, _ := obj["role"].(string)
	return strings.ToLower(role)
}

func responsesItemType(item any) string {
	obj, ok := item.(map[string]any)
	if !ok {
		return ""
	}
	typeName, _ := obj["type"].(string)
	return typeName
}

func responsesItemInputTypes(item any) map[types.FileType]bool {
	present := make(map[types.FileType]bool)
	markResponsesJSONInputTypes(item, present)
	return present
}

func markResponsesJSONInputTypes(value any, present map[types.FileType]bool) {
	switch item := value.(type) {
	case map[string]any:
		if typeName, ok := item["type"].(string); ok {
			switch typeName {
			case "input_image":
				present[types.FileTypeImage] = true
			case "input_file":
				present[types.FileTypeFile] = true
			}
		}
		for _, child := range item {
			markResponsesJSONInputTypes(child, present)
		}
	case []any:
		for _, child := range item {
			markResponsesJSONInputTypes(child, present)
		}
	}
}

func applyClaudeInputModalityPolicy(request *dto.ClaudeRequest, disabled map[types.FileType]bool) error {
	if request == nil {
		return nil
	}

	if !request.IsStringSystem() {
		keptSystem := make([]dto.ClaudeMediaMessage, 0)
		for _, media := range request.ParseSystem() {
			if media.Type == "image" && disabled[types.FileTypeImage] {
				continue
			}
			keptSystem = append(keptSystem, media)
		}
		if request.System != nil {
			request.System = keptSystem
		}
	}

	if len(request.Messages) == 0 {
		return nil
	}

	latestIdx := -1
	for i := len(request.Messages) - 1; i >= 0; i-- {
		if strings.EqualFold(request.Messages[i].Role, "user") {
			latestIdx = i
			break
		}
	}
	if latestIdx < 0 {
		return nil
	}

	if name := firstDisabledType(claudeMessageInputTypes(request.Messages[latestIdx]), disabled); name != "" {
		return unsupportedInputTypeError(name)
	}

	drop := make([]bool, len(request.Messages))
	for i := 0; i < latestIdx; {
		if firstDisabledType(claudeMessageInputTypes(request.Messages[i]), disabled) == "" {
			i++
			continue
		}
		right := i + 1
		for right < latestIdx && !strings.EqualFold(request.Messages[right].Role, "user") {
			right++
		}
		for j := i; j < right; j++ {
			drop[j] = true
		}
		i = right
	}

	kept := make([]dto.ClaudeMessage, 0, len(request.Messages))
	for i, message := range request.Messages {
		if drop[i] {
			continue
		}
		kept = append(kept, message)
	}
	request.Messages = kept
	return nil
}

func claudeMessageInputTypes(message dto.ClaudeMessage) map[types.FileType]bool {
	present := make(map[types.FileType]bool)
	if message.IsStringContent() {
		return present
	}
	content, _ := message.ParseContent()
	for _, media := range content {
		if media.Type == "image" {
			present[types.FileTypeImage] = true
		}
	}
	return present
}

func applyGeminiInputModalityPolicy(request *dto.GeminiChatRequest, disabled map[types.FileType]bool) error {
	if request == nil {
		return nil
	}

	if request.SystemInstructions != nil {
		keptParts := make([]dto.GeminiPart, 0, len(request.SystemInstructions.Parts))
		for _, part := range request.SystemInstructions.Parts {
			present := make(map[types.FileType]bool)
			markGeminiPartsInputTypes([]dto.GeminiPart{part}, present)
			if firstDisabledType(present, disabled) != "" {
				continue
			}
			keptParts = append(keptParts, part)
		}
		request.SystemInstructions.Parts = keptParts
	}

	if len(request.Contents) == 0 {
		return nil
	}

	latestIdx := findGeminiLatestUserIndex(request.Contents)
	if latestIdx < 0 {
		return nil
	}

	latestPresent := make(map[types.FileType]bool)
	markGeminiPartsInputTypes(request.Contents[latestIdx].Parts, latestPresent)
	if name := firstDisabledType(latestPresent, disabled); name != "" {
		return unsupportedInputTypeError(name)
	}

	drop := make([]bool, len(request.Contents))
	for i := 0; i < latestIdx; {
		present := make(map[types.FileType]bool)
		markGeminiPartsInputTypes(request.Contents[i].Parts, present)
		if firstDisabledType(present, disabled) == "" {
			i++
			continue
		}
		right := i + 1
		for right < latestIdx && !isGeminiUserRole(request.Contents[right].Role) {
			right++
		}
		for j := i; j < right; j++ {
			drop[j] = true
		}
		i = right
	}

	kept := make([]dto.GeminiChatContent, 0, len(request.Contents))
	for i, content := range request.Contents {
		if drop[i] {
			continue
		}
		kept = append(kept, content)
	}
	request.Contents = kept
	return nil
}

func findGeminiLatestUserIndex(contents []dto.GeminiChatContent) int {
	for i := len(contents) - 1; i >= 0; i-- {
		if isGeminiUserRole(contents[i].Role) {
			return i
		}
	}
	for i := len(contents) - 1; i >= 0; i-- {
		if contents[i].Role == "" {
			return i
		}
	}
	return -1
}

func isGeminiUserRole(role string) bool {
	return strings.EqualFold(role, "user")
}

func markGeminiPartsInputTypes(parts []dto.GeminiPart, present map[types.FileType]bool) {
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

// collectRequestInputTypes is retained for unit tests that assert detection helpers.
func collectRequestInputTypes(request dto.Request) map[types.FileType]bool {
	present := make(map[types.FileType]bool)
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		for _, message := range r.Messages {
			for k, v := range chatMessageInputTypes(message) {
				if v {
					present[k] = true
				}
			}
		}
	case *dto.OpenAIResponsesRequest:
		if len(r.Input) == 0 {
			return present
		}
		var value any
		if err := common.Unmarshal(r.Input, &value); err != nil {
			return present
		}
		markResponsesJSONInputTypes(value, present)
	case *dto.ClaudeRequest:
		if !r.IsStringSystem() {
			for _, media := range r.ParseSystem() {
				if media.Type == "image" {
					present[types.FileTypeImage] = true
				}
			}
		}
		for _, message := range r.Messages {
			for k, v := range claudeMessageInputTypes(message) {
				if v {
					present[k] = true
				}
			}
		}
	case *dto.GeminiChatRequest:
		for _, content := range r.Contents {
			markGeminiPartsInputTypes(content.Parts, present)
		}
		if r.SystemInstructions != nil {
			markGeminiPartsInputTypes(r.SystemInstructions.Parts, present)
		}
	}
	return present
}
