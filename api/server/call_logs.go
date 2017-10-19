package server

import (
	"net/http"

	"github.com/fnproject/fn/api"
	"github.com/gin-gonic/gin"
)

func (s *Server) handleCallLogGet(c *gin.Context) {
	ctx := c.Request.Context()

	appName := c.MustGet(api.AppName).(string)
	callID := c.Param(api.Call)
	log, err := s.LogDB.GetLog(ctx, appName, callID)
	if err != nil {
		handleErrorResponse(c, err)
		return
	}

	var isGZIP bool
	gzipped, err := gzip.NewReader(log)
	if err == nil {
		log = gzipped
		isGZIP = true
	}

	_, gzipped := log.(*gzip.Reader)

	for _, h := range req.Header["Accept-Encoding"] {
		if h == "gzip" && gzipped {
			io.Copy(c.Writer, log)
			return
		}
	}

	callObj := models.CallLog{
		CallID:  callID,
		AppName: appName,
		Log:     log,
	}

	c.JSON(http.StatusOK, callLogResponse{"Successfully loaded log", callObj})
}

func (s *Server) handleCallLogDelete(c *gin.Context) {
	ctx := c.Request.Context()

	appName := c.MustGet(api.AppName).(string)
	callID := c.Param(api.Call)
	err := s.LogDB.DeleteLog(ctx, appName, callID)
	if err != nil {
		handleErrorResponse(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"message": "Log delete accepted"})
}
