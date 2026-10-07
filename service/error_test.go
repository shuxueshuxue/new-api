package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResetStatusCode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		statusCode       int
		statusCodeConfig string
		expectedCode     int
	}{
		{
			name:             "map string value",
			statusCode:       429,
			statusCodeConfig: `{"429":"503"}`,
			expectedCode:     503,
		},
		{
			name:             "map int value",
			statusCode:       429,
			statusCodeConfig: `{"429":503}`,
			expectedCode:     503,
		},
		{
			name:             "skip invalid string value",
			statusCode:       429,
			statusCodeConfig: `{"429":"bad-code"}`,
			expectedCode:     429,
		},
		{
			name:             "skip status code 200",
			statusCode:       200,
			statusCodeConfig: `{"200":503}`,
			expectedCode:     200,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			newAPIError := &types.NewAPIError{
				StatusCode: tc.statusCode,
			}
			ResetStatusCode(newAPIError, tc.statusCodeConfig)
			require.Equal(t, tc.expectedCode, newAPIError.StatusCode)
		})
	}
}

func TestRelayErrorHandlerTruncatesInvalidJSONBodyInLog(t *testing.T) {
	withDebugEnabled(t, false)

	body := strings.Repeat("b", common.LocalLogContentLimit+256)
	var logBuffer bytes.Buffer

	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		common.LogWriterMu.Unlock()
	})

	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, "bad response status code 500", newAPIError.Error())
	require.Contains(t, logBuffer.String(), "[truncated")
	require.Contains(t, logBuffer.String(), fmt.Sprintf("original_length=%d", len(body)))
	require.NotContains(t, logBuffer.String(), strings.Repeat("b", common.LocalLogContentLimit+1))
}

func TestRelayErrorHandlerKeepsStructuredErrorMessage(t *testing.T) {
	message := strings.Repeat("c", common.LocalLogContentLimit+256)
	body := `{"message":"` + message + `"}`
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, message, newAPIError.Error())
}

func TestRelayErrorHandlerKeepsOpenAIErrorMessage(t *testing.T) {
	message := strings.Repeat("d", common.LocalLogContentLimit+256)
	body := `{"error":{"message":"` + message + `","type":"server_error","code":"server_error"}}`
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, message, newAPIError.Error())
}

func TestRelayErrorHandlerUnwrapsHapiUpstreamRejection(t *testing.T) {
	t.Parallel()

	hapiError := func(wrapped string) string {
		return `{"type":"error","error":{"type":"hapi_upstream_error","code":"hapi_upstream_error","message":` + strconv.Quote("HAPI upstream error: "+wrapped) + `}}`
	}
	testCases := []struct {
		name        string
		statusCode  int
		body        string
		wantStatus  int
		wantMessage string
		wantType    string
	}{
		{
			name:        "wrapped 400 becomes the rejection it is",
			statusCode:  http.StatusInternalServerError,
			body:        hapiError(`400 {"error":{"message":"role 'system' is not supported on this model.","type":"InvalidParameter","code":"InvalidParameter"},"trace_id":"c0d2e7b1","request_id":"3bcd4325"}`),
			wantStatus:  http.StatusBadRequest,
			wantMessage: "role 'system' is not supported on this model.",
			wantType:    "InvalidParameter",
		},
		{
			name:        "wrapped Anthropic error keeps its type",
			statusCode:  http.StatusInternalServerError,
			body:        hapiError(`400 {"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long"}}`),
			wantStatus:  http.StatusBadRequest,
			wantMessage: "prompt is too long",
			wantType:    "invalid_request_error",
		},
		{
			name:        "wrapped 401 is hapi's own account and stays a server error",
			statusCode:  http.StatusInternalServerError,
			body:        hapiError(`401 {"error":{"message":"invalid api key","type":"authentication_error"}}`),
			wantStatus:  http.StatusInternalServerError,
			wantMessage: `HAPI upstream error: 401 {"error":{"message":"invalid api key","type":"authentication_error"}}`,
			wantType:    "hapi_upstream_error",
		},
		{
			name:        "wrapped 400 without an error object stays a server error",
			statusCode:  http.StatusInternalServerError,
			body:        hapiError(`400 Bad Request`),
			wantStatus:  http.StatusInternalServerError,
			wantMessage: "HAPI upstream error: 400 Bad Request",
			wantType:    "hapi_upstream_error",
		},
		{
			name:        "wrapped 5xx stays a server error",
			statusCode:  http.StatusInternalServerError,
			body:        hapiError(`503 {"error":{"message":"overloaded","type":"server_error"}}`),
			wantStatus:  http.StatusInternalServerError,
			wantMessage: `HAPI upstream error: 503 {"error":{"message":"overloaded","type":"server_error"}}`,
			wantType:    "hapi_upstream_error",
		},
		{
			name:        "another upstream's 500 is left alone",
			statusCode:  http.StatusInternalServerError,
			body:        `{"error":{"message":"HAPI upstream error: 400 {}","type":"server_error","code":"server_error"}}`,
			wantStatus:  http.StatusInternalServerError,
			wantMessage: "HAPI upstream error: 400 {}",
			wantType:    "server_error",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resp := &http.Response{StatusCode: tc.statusCode, Body: io.NopCloser(strings.NewReader(tc.body))}
			newAPIError := RelayErrorHandler(context.Background(), resp, false)

			require.NotNil(t, newAPIError)
			assert.Equal(t, tc.wantStatus, newAPIError.StatusCode)
			assert.Equal(t, tc.wantMessage, newAPIError.Error())
			assert.Equal(t, tc.wantType, newAPIError.ToClaudeError().Type)
		})
	}
}

func TestRelayErrorHandlerKeepsInvalidJSONBodyInDebugLog(t *testing.T) {
	withDebugEnabled(t, true)

	body := strings.Repeat("e", common.LocalLogContentLimit+256)
	var logBuffer bytes.Buffer

	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		common.LogWriterMu.Unlock()
	})

	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.NotContains(t, logBuffer.String(), "[truncated")
	require.Contains(t, logBuffer.String(), body)
}

func withDebugEnabled(t *testing.T, enabled bool) {
	t.Helper()

	oldDebug := common.DebugEnabled
	common.DebugEnabled = enabled
	t.Cleanup(func() {
		common.DebugEnabled = oldDebug
	})
}
