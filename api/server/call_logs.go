package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"

	"github.com/fnproject/fn/api"
	"github.com/fnproject/fn/api/models"
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

	ungzip, err := gzip.NewReader(log)
	isGZIP := err == nil

	// TODO we _could_ store the gzipped / ungzipped length so we can set the
	// content length... leaving as TODO because any log store worth a damn will
	// stream with -1 most likely and it would be totally rad to stream gzip all
	// the way out without decompressing and recompressing (so many byte puppers
	// saved!)

	// make sure client can accept plain, then see about gzip
	for _, ah := range c.Request.Header["Accept"] {
		if ah == "text/plain" {
			c.Writer.Header().Set("Content-Type", "text/plain")
			c.Writer.Header().Set("Content-Length", "-1")
			for _, ae := range c.Request.Header["Accept-Encoding"] {
				if ae == "gzip" && isGZIP {
					c.Writer.Header().Set("Content-Encoding", "gzip")
					// copy as gzipped over the wire
					io.Copy(c.Writer, log)
					return
				}
			}
			// uncompress the gzip to the wire since they can't read it
			io.Copy(c.Writer, ungzip)
			return
		}
	}

	// otherwise default to json (TODO deprecate this probably? instead of making efficient..)
	var b bytes.Buffer
	b.ReadFrom(ungzip)

	callObj := models.CallLog{
		CallID:  callID,
		AppName: appName,
		Log:     b.String(),
	}

	c.JSON(http.StatusOK, callLogResponse{"Successfully loaded log", &callObj})
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
