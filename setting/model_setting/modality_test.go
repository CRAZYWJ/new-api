package model_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLookupInputModalities(t *testing.T) {
	original := *GetGlobalSettings()
	t.Cleanup(func() {
		*GetGlobalSettings() = original
	})

	imageOff := common.GetPointer(false)
	GetGlobalSettings().InputModalityPolicy = InputModalityPolicy{
		Enabled: true,
		Models: map[string]InputModalities{
			"deepseek-v4-flash": {Image: imageOff},
			"deepseek-v4-pro":   {Image: imageOff},
			"deepseek-chat":     {Image: imageOff},
		},
	}

	t.Run("exact match", func(t *testing.T) {
		caps, ok := LookupInputModalities("deepseek-v4-flash")
		require.True(t, ok)
		require.NotNil(t, caps.Image)
		assert.False(t, *caps.Image)
	})

	t.Run("case insensitive", func(t *testing.T) {
		caps, ok := LookupInputModalities("DeepSeek-V4-Pro")
		require.True(t, ok)
		require.NotNil(t, caps.Image)
		assert.False(t, *caps.Image)
	})

	t.Run("effort suffix", func(t *testing.T) {
		caps, ok := LookupInputModalities("deepseek-v4-flash-max")
		require.True(t, ok)
		require.NotNil(t, caps.Image)
		assert.False(t, *caps.Image)
	})

	t.Run("vendor prefix", func(t *testing.T) {
		caps, ok := LookupInputModalities("deepseek/deepseek-v4-flash")
		require.True(t, ok)
		require.NotNil(t, caps.Image)
		assert.False(t, *caps.Image)
	})

	t.Run("hyphen prefix match", func(t *testing.T) {
		caps, ok := LookupInputModalities("deepseek-v4-flash-none")
		require.True(t, ok)
		require.NotNil(t, caps.Image)
		assert.False(t, *caps.Image)
	})

	t.Run("unlisted model is not validated", func(t *testing.T) {
		_, ok := LookupInputModalities("gpt-4o")
		assert.False(t, ok)
	})

	t.Run("disabled policy", func(t *testing.T) {
		GetGlobalSettings().InputModalityPolicy.Enabled = false
		_, ok := LookupInputModalities("deepseek-v4-flash")
		assert.False(t, ok)
	})
}

func TestLookupInputModalitiesLongestPrefixWins(t *testing.T) {
	original := *GetGlobalSettings()
	t.Cleanup(func() {
		*GetGlobalSettings() = original
	})

	GetGlobalSettings().InputModalityPolicy = InputModalityPolicy{
		Enabled: true,
		Models: map[string]InputModalities{
			"deepseek-v4": {Image: common.GetPointer(false)},
			"deepseek-v4-flash": {
				Image: common.GetPointer(true),
			},
		},
	}

	caps, ok := LookupInputModalities("deepseek-v4-flash-max")
	require.True(t, ok)
	require.NotNil(t, caps.Image)
	assert.True(t, *caps.Image)
}
