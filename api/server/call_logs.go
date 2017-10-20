package server

import (
	"bytes"
	"compress/gzip"
	"fmt"
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

	fmt.Println(io.Copy(c.Writer, log))
	return

	ungzip, err := gzip.NewReader(log)
	isGZIP := err == nil
	fmt.Println("YODAWG", isGZIP, err, ungzip == nil)

	// TODO we _could_ store the gzipped / ungzipped length so we can set the
	// content length... leaving as TODO because any log store worth a damn will
	// stream with -1 most likely and it would be totally rad to stream gzip all
	// the way out without decompressing and recompressing (so many byte puppers
	// saved!)

	// make sure client can accept plain, then see about gzip
	for _, ah := range c.Request.Header["Accept"] {
		if ah == "text/plain" {
			c.Writer.Header().Set("Content-Type", "text/plain")
			// c.Writer.Header().Set("Content-Length", "-1") // go barfs on this
			for _, ae := range c.Request.Header["Accept-Encoding"] {
				if ae == "gzip" && isGZIP {
					c.Writer.Header().Set("Content-Encoding", "gzip")
					// copy as gzipped over the wire
					io.Copy(c.Writer, log)
					return
				}
			}

			// they don't accept gzip or the log is not gzipped, so send as plain
			if isGZIP {
				fmt.Println(io.Copy(c.Writer, ungzip))
			} else {
				io.Copy(c.Writer, log)
			}
			return
		}
	}

	// otherwise default to json (TODO deprecate this probably? instead of making efficient..)
	var b bytes.Buffer
	if isGZIP {
		b.ReadFrom(ungzip)
	} else {
		b.ReadFrom(log)
	}

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
