package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRelayTaskFetchRejectsUnknownMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())

	taskErr := RelayTaskFetch(context, -1)

	require.NotNil(t, taskErr)
	require.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	require.Equal(t, "invalid_relay_mode", taskErr.Code)
}
