package dto

import (
	"testing"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/require"
)

func TestEpochSecondsUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    EpochSeconds
		wantErr bool
	}{
		{name: "integer", input: `1787207287`, want: 1787207287},
		{name: "float whole", input: `1787207287.0`, want: 1787207287},
		{name: "float fractional", input: `1787207287.75`, want: 1787207288},
		{name: "numeric string", input: `"1787207287.0"`, want: 1787207287},
		{name: "null", input: `null`, want: 0},
		{name: "invalid", input: `12x`, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got EpochSeconds
			err := kitutil.Unmarshal([]byte(tc.input), &got)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestEpochSecondsMarshalAsInteger(t *testing.T) {
	out, err := kitutil.Marshal(EpochSeconds(1787207287))
	require.NoError(t, err)
	require.Equal(t, `1787207287`, string(out))
}

// Regression: upstreams returning a fractional created_at used to fail
// unmarshaling the whole stream event, dropping response.created /
// in_progress / completed and making clients retry with repeated answers.
func TestResponsesStreamResponseAcceptsFractionalCreatedAt(t *testing.T) {
	var got ResponsesStreamResponse
	require.NoError(t, kitutil.Unmarshal(
		[]byte(`{"type":"response.created","response":{"id":"resp_1","object":"response","created_at":1787207287.75,"status":"in_progress","model":"deepseek-v4-flash","output":[]}}`),
		&got,
	))
	require.NotNil(t, got.Response)
	require.Equal(t, EpochSeconds(1787207288), got.Response.CreatedAt)
}
