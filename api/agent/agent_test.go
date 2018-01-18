package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fnproject/fn/api/datastore"
	"github.com/fnproject/fn/api/models"
	"github.com/fnproject/fn/api/mqs"
	"github.com/sirupsen/logrus"
)

func init() {
	// TODO figure out some sane place to stick this
	formatter := &logrus.TextFormatter{
		FullTimestamp: true,
	}
	logrus.SetFormatter(formatter)
	logrus.SetLevel(logrus.DebugLevel)
}

// TODO need to add at least one test for our cachy cache

func checkExpectedHeaders(t *testing.T, expectedHeaders http.Header, receivedHeaders http.Header) {

	checkMap := make([]string, 0, len(expectedHeaders))
	for k, _ := range expectedHeaders {
		checkMap = append(checkMap, k)
	}

	for k, vs := range receivedHeaders {
		for i, v := range expectedHeaders[k] {
			if i >= len(vs) || vs[i] != v {
				t.Fatal("header mismatch", k, vs)
			}
		}

		for i, _ := range checkMap {
			if checkMap[i] == k {
				checkMap = append(checkMap[:i], checkMap[i+1:]...)
				break
			}
		}
	}

	if len(checkMap) > 0 {
		t.Fatalf("expected headers not found=%v", checkMap)
	}
}

func TestCallConfigurationRequest(t *testing.T) {
	appName := "myapp"
	path := "/sleeper"
	image := "fnproject/sleeper"
	const timeout = 1
	const idleTimeout = 20
	const memory = 256
	typ := "sync"
	format := "default"

	cfg := models.Config{"APP_VAR": "FOO"}
	rCfg := models.Config{"ROUTE_VAR": "BAR"}

	ds := datastore.NewMockInit(
		[]*models.App{
			{Name: appName, Config: cfg},
		},
		[]*models.Route{
			{
				Config:      rCfg,
				Path:        path,
				AppName:     appName,
				Image:       image,
				Type:        typ,
				Format:      format,
				Timeout:     timeout,
				IdleTimeout: idleTimeout,
				Memory:      memory,
			},
		}, nil,
	)

	a := New(NewDirectDataAccess(ds, ds, new(mqs.Mock)))
	defer a.Close()

	w := httptest.NewRecorder()

	method := "GET"
	url := "http://127.0.0.1:8080/r/" + appName + path
	payload := "payload"
	contentLength := strconv.Itoa(len(payload))
	req, err := http.NewRequest(method, url, strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Add("MYREALHEADER", "FOOLORD")
	req.Header.Add("MYREALHEADER", "FOOPEASANT")
	req.Header.Add("Content-Length", contentLength)
	req.Header.Add("FN_PATH", "thewrongroute") // ensures that this doesn't leak out, should be overwritten

	call, err := a.GetCall(
		WithWriter(w), // XXX (reed): order matters [for now]
		FromRequest(appName, path, req),
	)
	if err != nil {
		t.Fatal(err)
	}

	model := call.Model()

	// make sure the values are all set correctly
	if model.ID == "" {
		t.Fatal("model does not have id, GetCall should assign id")
	}
	if model.AppName != appName {
		t.Fatal("app name mismatch", model.AppName, appName)
	}
	if model.Path != path {
		t.Fatal("path mismatch", model.Path, path)
	}
	if model.Image != image {
		t.Fatal("image mismatch", model.Image, image)
	}
	if model.Type != "sync" {
		t.Fatal("route type mismatch", model.Type)
	}
	if model.Priority == nil {
		t.Fatal("GetCall should make priority non-nil so that async works because for whatever reason some clowns plumbed it all over the mqs even though the user can't specify it gg")
	}
	if model.Timeout != timeout {
		t.Fatal("timeout mismatch", model.Timeout, timeout)
	}
	if model.IdleTimeout != idleTimeout {
		t.Fatal("idle timeout mismatch", model.IdleTimeout, idleTimeout)
	}
	if time.Time(model.CreatedAt).IsZero() {
		t.Fatal("GetCall should stamp CreatedAt, got nil timestamp")
	}
	if model.URL != url {
		t.Fatal("url mismatch", model.URL, url)
	}
	if model.Method != method {
		t.Fatal("method mismatch", model.Method, method)
	}
	if model.Payload != "" { // NOTE: this is expected atm
		t.Fatal("GetCall FromRequest should not fill payload, got non-empty payload", model.Payload)
	}

	expectedConfig := map[string]string{
		"FN_FORMAT":   format,
		"FN_APP_NAME": appName,
		"FN_PATH":     path,
		"FN_MEMORY":   strconv.Itoa(memory),
		"FN_TYPE":     typ,
		"APP_VAR":     "FOO",
		"ROUTE_VAR":   "BAR",
	}

	for k, v := range expectedConfig {
		if v2 := model.Config[k]; v2 != v {
			t.Fatal("config mismatch", k, v, v2, model.Config)
		}
		delete(expectedConfig, k)
	}

	if len(expectedConfig) > 0 {
		t.Fatal("got extra vars in config set, add me to tests ;)", expectedConfig)
	}

	expectedHeaders := make(http.Header)
	expectedHeaders.Add("FN_CALL_ID", model.ID)
	expectedHeaders.Add("FN_METHOD", method)
	expectedHeaders.Add("FN_REQUEST_URL", url)

	expectedHeaders.Add("MYREALHEADER", "FOOLORD")
	expectedHeaders.Add("MYREALHEADER", "FOOPEASANT")
	expectedHeaders.Add("Content-Length", contentLength)

	checkExpectedHeaders(t, expectedHeaders, model.Headers)

	// TODO check response writer for route headers
}

func TestCallConfigurationModel(t *testing.T) {
	appName := "myapp"
	path := "/sleeper"
	image := "fnproject/sleeper"
	const timeout = 1
	const idleTimeout = 20
	const memory = 256
	// CPUs := models.MilliCPUs(1000)
	method := "GET"
	url := "http://127.0.0.1:8080/r/" + appName + path
	payload := "payload"
	typ := "sync"
	format := "default"
	cfg := models.Config{
		"FN_FORMAT":   format,
		"FN_APP_NAME": appName,
		"FN_PATH":     path,
		"FN_MEMORY":   strconv.Itoa(memory),
		// "FN_CPUS":     CPUs.String(),
		"FN_TYPE":   typ,
		"APP_VAR":   "FOO",
		"ROUTE_VAR": "BAR",
	}

	cm := &models.Call{
		Config:      cfg,
		AppName:     appName,
		Path:        path,
		Image:       image,
		Type:        typ,
		Format:      format,
		Timeout:     timeout,
		IdleTimeout: idleTimeout,
		Memory:      memory,
		// CPUs:        CPUs,
		Payload: payload,
		URL:     url,
		Method:  method,
	}

	// FromModel doesn't need a datastore, for now...
	ds := datastore.NewMockInit(nil, nil, nil)

	a := New(NewDirectDataAccess(ds, ds, new(mqs.Mock)))
	defer a.Close()

	callI, err := a.GetCall(FromModel(cm))
	if err != nil {
		t.Fatal(err)
	}

	req := callI.(*call).req

	var b bytes.Buffer
	io.Copy(&b, req.Body)

	if b.String() != payload {
		t.Fatal("expected payload to match, but it was a lie")
	}
}

func TestAsyncCallHeaders(t *testing.T) {
	appName := "myapp"
	path := "/sleeper"
	image := "fnproject/sleeper"
	const timeout = 1
	const idleTimeout = 20
	const memory = 256
	CPUs := models.MilliCPUs(200)
	method := "GET"
	url := "http://127.0.0.1:8080/r/" + appName + path
	payload := "payload"
	typ := "async"
	format := "http"
	contentType := "suberb_type"
	contentLength := strconv.FormatInt(int64(len(payload)), 10)
	config := map[string]string{
		"FN_FORMAT":   format,
		"FN_APP_NAME": appName,
		"FN_PATH":     path,
		"FN_MEMORY":   strconv.Itoa(memory),
		"FN_CPUS":     CPUs.String(),
		"FN_TYPE":     typ,
		"APP_VAR":     "FOO",
		"ROUTE_VAR":   "BAR",
		"DOUBLE_VAR":  "BIZ, BAZ",
	}
	headers := map[string][]string{
		// FromRequest would insert these from original HTTP request
		"Content-Type":   []string{contentType},
		"Content-Length": []string{contentLength},
	}

	cm := &models.Call{
		Config:      config,
		Headers:     headers,
		AppName:     appName,
		Path:        path,
		Image:       image,
		Type:        typ,
		Format:      format,
		Timeout:     timeout,
		IdleTimeout: idleTimeout,
		Memory:      memory,
		CPUs:        CPUs,
		Payload:     payload,
		URL:         url,
		Method:      method,
	}

	// FromModel doesn't need a datastore, for now...
	ds := datastore.NewMockInit(nil, nil, nil)

	a := New(NewDirectDataAccess(ds, ds, new(mqs.Mock)))
	defer a.Close()

	callI, err := a.GetCall(FromModel(cm))
	if err != nil {
		t.Fatal(err)
	}

	// make sure headers seem reasonable
	req := callI.(*call).req

	// These should be here based on payload length and/or fn_header_* original headers
	expectedHeaders := make(http.Header)
	expectedHeaders.Set("Content-Type", contentType)
	expectedHeaders.Set("Content-Length", strconv.FormatInt(int64(len(payload)), 10))

	checkExpectedHeaders(t, expectedHeaders, req.Header)

	var b bytes.Buffer
	io.Copy(&b, req.Body)

	if b.String() != payload {
		t.Fatal("expected payload to match, but it was a lie")
	}
}

func TestLoggerIsStringerAndWorks(t *testing.T) {
	// TODO test limit writer, logrus writer, etc etc

	loggyloo := logrus.WithFields(logrus.Fields{"yodawg": true})
	logger := setupLogger(loggyloo)

	if _, ok := logger.(fmt.Stringer); !ok {
		// NOTE: if you are reading, maybe what you've done is ok, but be aware we were relying on this for optimization...
		t.Fatal("you turned the logger into something inefficient and possibly better all at the same time, how dare ye!")
	}

	str := "0 line\n1 line\n2 line\n\n4 line"
	logger.Write([]byte(str))

	strGot := logger.(fmt.Stringer).String()

	if strGot != str {
		t.Fatal("logs did not match expectations, like being an adult", strGot, str)
	}

	logger.Close() // idk maybe this would panic might as well call this

	// TODO we could check for the toilet to flush here to logrus
}

func TestSubmitError(t *testing.T) {
	appName := "myapp"
	path := "/error"
	image := "fnproject/error"
	const timeout = 10
	const idleTimeout = 20
	const memory = 256
	CPUs := models.MilliCPUs(200)
	method := "GET"
	url := "http://127.0.0.1:8080/r/" + appName + path
	payload := "payload"
	typ := "sync"
	format := "default"
	config := map[string]string{
		"FN_FORMAT":   format,
		"FN_APP_NAME": appName,
		"FN_PATH":     path,
		"FN_MEMORY":   strconv.Itoa(memory),
		"FN_CPUS":     CPUs.String(),
		"FN_TYPE":     typ,
		"APP_VAR":     "FOO",
		"ROUTE_VAR":   "BAR",
		"DOUBLE_VAR":  "BIZ, BAZ",
	}

	cm := &models.Call{
		Config:      config,
		AppName:     appName,
		Path:        path,
		Image:       image,
		Type:        typ,
		Format:      format,
		Timeout:     timeout,
		IdleTimeout: idleTimeout,
		Memory:      memory,
		CPUs:        CPUs,
		Payload:     payload,
		URL:         url,
		Method:      method,
	}

	// FromModel doesn't need a datastore, for now...
	ds := datastore.NewMockInit(nil, nil, nil)

	a := New(NewDirectDataAccess(ds, ds, new(mqs.Mock)))
	defer a.Close()

	callI, err := a.GetCall(FromModel(cm))
	if err != nil {
		t.Fatal(err)
	}

	err = a.Submit(callI)
	if err == nil {
		t.Fatal("expected error but got none")
	}

	if cm.Status != "error" {
		t.Fatal("expected status to be set to 'error' but was", cm.Status)
	}

	if cm.Error == "" {
		t.Fatal("expected error string to be set on call")
	}
}

// return a model with all fields filled in with fnproject/sleeper image, change as needed
func testCall() *models.Call {
	appName := "myapp"
	path := "/sleeper"
	image := "fnproject/fn-test-utils"
	const timeout = 10
	const idleTimeout = 20
	const memory = 256
	CPUs := models.MilliCPUs(200)
	method := "GET"
	url := "http://127.0.0.1:8080/r/" + appName + path
	payload := "payload"
	typ := "async"
	format := "http"
	contentType := "suberb_type"
	contentLength := strconv.FormatInt(int64(len(payload)), 10)
	config := map[string]string{
		"FN_FORMAT":   format,
		"FN_APP_NAME": appName,
		"FN_PATH":     path,
		"FN_MEMORY":   strconv.Itoa(memory),
		"FN_CPUS":     CPUs.String(),
		"FN_TYPE":     typ,
		"APP_VAR":     "FOO",
		"ROUTE_VAR":   "BAR",
		"DOUBLE_VAR":  "BIZ, BAZ",
	}
	headers := map[string][]string{
		// FromRequest would insert these from original HTTP request
		"Content-Type":   []string{contentType},
		"Content-Length": []string{contentLength},
	}

	return &models.Call{
		Config:      config,
		Headers:     headers,
		AppName:     appName,
		Path:        path,
		Image:       image,
		Type:        typ,
		Format:      format,
		Timeout:     timeout,
		IdleTimeout: idleTimeout,
		Memory:      memory,
		CPUs:        CPUs,
		Payload:     payload,
		URL:         url,
		Method:      method,
	}
}

func TestPipesAreClear(t *testing.T) {
	// The basic idea here is to make a call start a hot container, and the
	// first call has a reader that only reads after a delay, which is beyond
	// the boundary of the first call's timeout. Then, run a second call
	// with a different body that also has a delay, in which time the first
	// call's reader is expected to read before the second's. make sure the
	// second call does not get the first call's body. This ensures the input
	// paths for calls do not overlap into the same container.
	// TODO for testing output we need to make sure the first call's writer doesn't get anything? think moar
	// TODO need to consider timings further, may be finicky in constrained environments (time for mock timings?)
	//
	// causal (seconds):
	// T1=start task one, T1TO=task one times out, T2=start task two
	// T1W=task one writes, T2W=task two writes
	//
	//
	//  1s  2   3    4   5   6
	// ---------------------------
	//
	// T1-------T1TO-T2--T1W-T2W

	// TODO echo image
	call := testCall()
	call.Type = "sync"
	call.Format = "http"
	call.IdleTimeout = 60 // keep this bad boy alive
	call.Timeout = 4      // short

	// we need to load in app & route so that FromRequest works
	ds := datastore.NewMockInit(
		[]*models.App{
			{Name: call.AppName},
		},
		[]*models.Route{
			{
				Path:        call.Path,
				AppName:     call.AppName,
				Image:       call.Image,
				Type:        call.Type,
				Format:      call.Format,
				Timeout:     call.Timeout,
				IdleTimeout: call.IdleTimeout,
				Memory:      call.Memory,
			},
		}, nil,
	)

	a := New(NewDirectDataAccess(ds, ds, new(mqs.Mock)))
	defer a.Close()

	// grab a buffer so we can read what gets written to this guy
	// use same output for both, to make sure we don't see the first response in there.
	// TODO shouldn't we make 2 and then check both after the 2nd completes instead of intermingling?
	var outtie bytes.Buffer

	// read this body after 5s (after call times out)
	bodOne := `{"echoContent":"yodawg"}`
	delayBodyOne := &delayReader{inny: strings.NewReader(bodOne), delay: 5 * time.Second}

	req, err := http.NewRequest("GET", call.URL, delayBodyOne)
	if err != nil {
		t.Fatal("unexpected error building request", err)
	}

	callI, err := a.GetCall(FromRequest(call.AppName, call.Path, req), WithWriter(&outtie))
	if err != nil {
		t.Fatal(err)
	}

	// this will time out after 4s, our reader reads after 5s
	t.Log("before submit one:", time.Now())
	err = a.Submit(callI)
	t.Log("after submit one:", time.Now())
	if err == nil {
		t.Error("expected error but got none")
	}
	t.Log("first guy err:", err)

	// TODO we need to make sure we got an http parsing error here (and that it's user friendly for fdks?)

	// if we submit the same call to the hot container again,
	// this can be finicky if the
	// hot logic simply fails to re-use a container then this will
	// 'just work' but at one point this failed.

	// only delay this body 2 seconds, so that we read at 6s (first writes at 5s) before time out
	bodTwo := `{"echoContent":"NODAWG"}`
	delayBodyTwo := &delayReader{inny: strings.NewReader(bodTwo), delay: 2 * time.Second}

	req, err = http.NewRequest("GET", call.URL, delayBodyTwo)
	if err != nil {
		t.Fatal("unexpected error building request", err)
	}

	callI, err = a.GetCall(FromRequest(call.AppName, call.Path, req), WithWriter(&outtie))
	if err != nil {
		t.Fatal(err)
	}

	t.Log("before submit two:", time.Now())
	err = a.Submit(callI)
	t.Log("after submit two:", time.Now())
	if err != nil {
		// NOTE: this shouldn't error here, but it may because the first call reader gets read
		// during this call's duration. don't do a Fatal so that we can read the body to see
		// what really happened
		t.Error("got error from submit when task should succeed")
	}

	// we're using http format so this will have written a whole http request
	res, err := http.ReadResponse(bufio.NewReader(&outtie), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	// {"request":{},"header":{"Fn_call_id":["01C32SA8WX0000600000000000"]
	var resp struct {
		H http.Header `json:"header"`
	}

	json.NewDecoder(res.Body).Decode(&resp)

	rid := resp.H.Get("FN_CALL_ID")

	if rid != callI.Model().ID {
		t.Fatalf("body from second call was not what we wanted. boo. got wrong id: %v wanted: %v", rid, callI.Model().ID)
	}
}

type delayReader struct {
	once  sync.Once
	inny  io.Reader
	delay time.Duration
}

func (r *delayReader) Read(b []byte) (int, error) {
	r.once.Do(func() { time.Sleep(r.delay) })
	return r.inny.Read(b)
}
