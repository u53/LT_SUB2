//go:build unit

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestSystemHandlerGetVersionReturnsBuildVersion 验证版本接口只返回编译期注入的版本号。
//
// 本分支不提供在线更新/回滚：路由层已移除 /check-updates、/update、
// /rollback、/rollback-versions，只保留 /version 与 /restart。
func TestSystemHandlerGetVersionReturnsBuildVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewSystemHandler("2.12.2", nil)
	router := gin.New()
	router.GET("/api/v1/admin/system/version", handler.GetVersion)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/version", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Code int `json:"code"`
		Data struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, 0, body.Code)
	require.Equal(t, "2.12.2", body.Data.Version)
}

// 版本号为空时也不能 panic：镜像/开发构建可能没有注入版本。
func TestSystemHandlerGetVersionEmptyVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewSystemHandler("", nil)
	router := gin.New()
	router.GET("/api/v1/admin/system/version", handler.GetVersion)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/version", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}
