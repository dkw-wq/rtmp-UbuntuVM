package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Error codes
const (
	CodeSuccess      = 0
	CodeInvalidParam = 1001
	CodeNotFound     = 1002
	CodeInternal     = 1003
	CodeConflict     = 1004
)

// H is a shorthand for gin.H.
type H = gin.H

// Success returns {"code":0, "msg":"ok", "data":...} with HTTP 200.
func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, H{
		"code": CodeSuccess,
		"msg":  "ok",
		"data": data,
	})
}

// Error returns {"code":<code>, "msg":...} with the given HTTP status.
func Error(c *gin.Context, httpStatus int, code int, msg string) {
	c.AbortWithStatusJSON(httpStatus, H{
		"code": code,
		"msg":  msg,
	})
}
