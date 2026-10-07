package dto

import (
	"encoding/json"
	"strings"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

//type OpenAIError struct {
//	Message string `json:"message"`
//	Type    string `json:"type"`
//	Param   string `json:"param"`
//	Code    any    `json:"code"`
//}

type OpenAIErrorWithStatusCode struct {
	Error      types.OpenAIError `json:"error"`
	StatusCode int               `json:"status_code"`
	LocalError bool
}

type GeneralErrorResponse struct {
	Error    json.RawMessage `json:"error"`
	Message  string          `json:"message"`
	Msg      string          `json:"msg"`
	Err      string          `json:"err"`
	ErrorMsg string          `json:"error_msg"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
	Detail   string          `json:"detail,omitempty"`
	Header   struct {
		Message string `json:"message"`
	} `json:"header"`
	Response struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"response"`
}

func (e GeneralErrorResponse) TryToOpenAIError() *types.OpenAIError {
	var openAIError types.OpenAIError
	if len(e.Error) > 0 {
		err := kitutil.Unmarshal(e.Error, &openAIError)
		if err == nil && openAIError.Message != "" {
			return &openAIError
		}
	}
	return nil
}

// HapiUpstreamRejection reads the error that hapi, an upstream relay, wraps around a rejection from its
// own upstream: error type "hapi_upstream_error" with the message "HAPI upstream error: <status> <body>".
// hapi sends it as HTTP 500, or, on a streamed request, as an `error` event inside an HTTP 200 stream; the
// caller passes the error's type and message from whichever of the two it read. When the wrapped status is
// 400 the request itself was rejected, and it returns that error, so the client is told so instead of being
// told to retry a server error. Every other wrapped status stays hapi's server error: a wrapped 401, 403 or
// 429 is about hapi's own upstream account, not the client's key, and a wrapped 5xx is an upstream outage.
// A wrapped body that is not an error object also returns nil.
func HapiUpstreamRejection(errorType, message string) *types.OpenAIError {
	if errorType != "hapi_upstream_error" {
		return nil
	}
	body, ok := strings.CutPrefix(message, "HAPI upstream error: 400 ")
	if !ok {
		return nil
	}
	var inner GeneralErrorResponse
	if kitutil.Unmarshal([]byte(body), &inner) != nil {
		return nil
	}
	innerError := inner.TryToOpenAIError()
	if innerError == nil {
		return nil
	}
	// Anthropic-shaped errors carry no code; a Claude client reads the code as the error type.
	if innerError.Code == nil {
		innerError.Code = innerError.Type
	}
	return innerError
}

func (e GeneralErrorResponse) ToMessage() string {
	if len(e.Error) > 0 {
		switch kitutil.GetJsonType(e.Error) {
		case "object":
			var openAIError types.OpenAIError
			err := kitutil.Unmarshal(e.Error, &openAIError)
			if err == nil && openAIError.Message != "" {
				return openAIError.Message
			}
		case "string":
			var msg string
			err := kitutil.Unmarshal(e.Error, &msg)
			if err == nil && msg != "" {
				return msg
			}
		default:
			return string(e.Error)
		}
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Msg != "" {
		return e.Msg
	}
	if e.Err != "" {
		return e.Err
	}
	if e.ErrorMsg != "" {
		return e.ErrorMsg
	}
	if e.Detail != "" {
		return e.Detail
	}
	if e.Header.Message != "" {
		return e.Header.Message
	}
	if e.Response.Error.Message != "" {
		return e.Response.Error.Message
	}
	return ""
}
