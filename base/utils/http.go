package utils

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/lestrrat-go/backoff/v2"
	"github.com/pkg/errors"

	// used only in developer mode
	_ "net/http/pprof" //nolint:gosec
)

func HTTPCallRetry(httpCallFun func() (outputDataPtr interface{}, resp *http.Response, err error),
	exponentialRetry bool, maxRetries int, codesToRetry ...int) (outputDataPtr interface{}, err error) {
	ctx, cancel := context.WithCancel(context.Background())
	backoffState := startBackoff(ctx, exponentialRetry, maxRetries)
	defer cancel()
	attempt := 0
	for backoff.Continue(backoffState) {
		attempt++
		outDataPtr, resp, callErr := httpCallFun()
		if statusCodeFound(resp, codesToRetry) {
			LogWarn("attempt", attempt, "status_code", TryGetStatusCode(resp),
				"HTTP call ended with wrong status code")
			continue
		}

		if callErr == nil {
			return outDataPtr, nil
		}

		if len(codesToRetry) == 0 { // no "retry" codes specified, continue always
			LogWarn("attempt", attempt, "err", callErr.Error(), "HTTP call failed, trying again")
			continue
		}

		responseDetails := tryGetResponseDetails(resp)
		if resp != nil {
			resp.Body.Close()
		}
		return nil, errors.Wrap(callErr, "HTTP call failed"+responseDetails)
	}
	return nil, errors.Errorf("HTTP retry call failed, attempts: %d", attempt)
}

func CallAPI(client *http.Client, request *http.Request, debugEnabled bool) (*http.Response, error) {
	if debugEnabled {
		dump, err := httputil.DumpRequestOut(request, true)
		if err != nil {
			return nil, err
		}
		LogDebug("dump", fmt.Sprintf("\n%s\n", string(dump)), "http call")
	}

	resp, err := client.Do(request)
	if err != nil {
		return resp, err
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode > 299 {
		// we got non 2xx status code, return error
		// return also the response which is used for request retry
		return resp, fmt.Errorf("received non 2xx status code, status code: %d", resp.StatusCode)
	}

	if debugEnabled {
		dump, err := httputil.DumpResponse(resp, true)
		if err != nil {
			return resp, err
		}
		LogDebug("dump", fmt.Sprintf("\n%s\n", string(dump)), "http response")
	}
	return resp, err
}

func tryGetResponseDetails(response *http.Response) string {
	details := ""
	if response != nil {
		details = fmt.Sprintf(", status code: %d", response.StatusCode)
	}
	return details
}

func TryGetStatusCode(response *http.Response) int {
	if response == nil {
		return 0
	}
	return response.StatusCode
}

func startBackoff(ctx context.Context, exponential bool, maxRetries int) backoff.Controller {
	if exponential {
		var policy = backoff.Exponential(backoff.WithMinInterval(time.Second), backoff.WithMaxRetries(maxRetries))
		backoffState := policy.Start(ctx)
		return backoffState
	}
	var policy = backoff.Constant(backoff.WithInterval(time.Second), backoff.WithMaxRetries(maxRetries))
	backoffState := policy.Start(ctx)
	return backoffState
}

func statusCodeFound(response *http.Response, statusCodes []int) bool {
	if response == nil {
		return false
	}

	for _, code := range statusCodes {
		if code == response.StatusCode {
			return true
		}
	}
	return false
}

// run net/http/pprof on privatePort
func RunProfiler() {
	if CoreCfg.ProfilerEnabled {
		go func() {
			server := &http.Server{Addr: fmt.Sprintf(":%d", CoreCfg.PrivatePort), ReadHeaderTimeout: 120 * time.Second}
			err := server.ListenAndServe()
			if err != nil {
				LogWarn("err", err.Error(), "couldn't start profiler")
			}
		}()
	}
}
