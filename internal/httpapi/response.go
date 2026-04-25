package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	apperrors "github.com/ck-chat/ck-chat/internal/errors"
)

type Response struct {
	Data      any               `json:"data,omitempty"`
	Error     *ErrorResponse    `json:"error,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
	TraceID   string            `json:"trace_id,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
}

type ErrorResponse struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details,omitempty"`
}

func WriteJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Response{Data: data})
}

func WriteError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Response{Error: errorResponse(err)})
}

func GinJSON(c *gin.Context, status int, data any) {
	c.JSON(status, Response{Data: data})
}

func GinError(c *gin.Context, status int, err error) {
	c.JSON(status, Response{Error: errorResponse(err)})
}

func errorResponse(err error) *ErrorResponse {
	resp := ErrorResponse{Code: string(apperrors.SysInternal), Message: "internal error", Retryable: true}
	if appErr, ok := err.(apperrors.AppError); ok {
		resp.Code = string(appErr.Code)
		resp.Message = appErr.Message
		resp.Retryable = appErr.Retryable
	}
	return &resp
}
