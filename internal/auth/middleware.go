package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"

	apperrors "github.com/ck-chat/ck-chat/internal/errors"
	"github.com/ck-chat/ck-chat/internal/httpapi"
)

func Middleware(tokens *TokenService) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := BearerToken(c.GetHeader("Authorization"))
		if err != nil {
			httpapi.GinError(c, http.StatusUnauthorized, apperrors.AppError{Code: apperrors.AuthTokenInvalid, Message: err.Error(), Retryable: false})
			c.Abort()
			return
		}
		principal, err := tokens.Verify(token)
		if err != nil {
			httpapi.GinError(c, http.StatusUnauthorized, apperrors.AppError{Code: apperrors.AuthTokenInvalid, Message: err.Error(), Retryable: false})
			c.Abort()
			return
		}
		c.Request = c.Request.WithContext(ContextWithPrincipal(c.Request.Context(), principal))
		c.Set("principal", principal)
		c.Next()
	}
}
