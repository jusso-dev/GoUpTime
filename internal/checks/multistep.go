// Multi-step API flow monitor. Runs a sequence of HTTP requests with
// per-step assertions and variable extraction. Variables flow between
// steps via {{NAME}} template substitution in URLs, headers, and bodies.
//
// Use case: validate a login → fetch-user flow end-to-end, ensuring not
// only that each step returns 200 but that the response shape matches
// what downstream consumers expect.

package checks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/template"
	"time"

	"github.com/PaesslerAG/jsonpath"

	"github.com/jusso-dev/uptime/internal/models"
)

// MultistepStore is the slice of the repository.Store interface this
// checker needs. Narrow on purpose.
type MultistepStore interface {
	GetMultistepScript(ctx context.Context, monitorID string) (models.MultistepScript, error)
}

type MultistepChecker struct {
	Options Options
	Store   MultistepStore
}

func (c MultistepChecker) Check(ctx context.Context, monitor models.Monitor) (models.CheckResult, error) {
	result := baseResult(monitor)
	if c.Store == nil {
		err := fmt.Errorf("multistep checker is missing its store")
		result.Error = err.Error()
		return result, err
	}
	script, err := c.Store.GetMultistepScript(ctx, monitor.ID)
	if err != nil {
		result.Error = fmt.Sprintf("multistep config missing: %v", err)
		return result, err
	}
	if len(script.Steps.Steps) == 0 {
		result.Error = "multistep script has no steps"
		return result, nil
	}

	timeout := TimeoutFor(monitor, c.Options.DefaultTimeout)
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := &http.Client{Timeout: timeout}
	vars := make(map[string]string, len(script.Steps.Vars)+8)
	for k, v := range script.Steps.Vars {
		vars[k] = v
	}

	start := time.Now()
	for i, step := range script.Steps.Steps {
		stepStart := time.Now()
		if err := runMultistepStep(checkCtx, client, vars, step, c.Options.UserAgent); err != nil {
			result.Error = fmt.Sprintf("step %d (%s): %v", i+1, displayName(step, i), err)
			result.ResponseTimeMS = time.Since(start).Milliseconds()
			result.TotalMS = result.ResponseTimeMS
			result.TimeToFirstByteMS = time.Since(stepStart).Milliseconds()
			return result, nil
		}
	}
	result.CheckedAt = time.Now().UTC()
	result.ResponseTimeMS = time.Since(start).Milliseconds()
	result.TotalMS = result.ResponseTimeMS
	result.Success = true
	result.Status = models.StatusUp
	result.StatusCode = 200
	return result, nil
}

func runMultistepStep(ctx context.Context, client *http.Client, vars map[string]string, step models.MultistepStep, userAgent string) error {
	method := step.Method
	if method == "" {
		method = http.MethodGet
	}
	url, err := substitute(step.URL, vars)
	if err != nil {
		return fmt.Errorf("substitute url: %w", err)
	}
	body, err := substitute(step.Body, vars)
	if err != nil {
		return fmt.Errorf("substitute body: %w", err)
	}
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	for k, v := range step.Headers {
		expanded, err := substitute(v, vars)
		if err != nil {
			return fmt.Errorf("substitute header %s: %w", k, err)
		}
		req.Header.Set(k, expanded)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	var jsonBody any
	if len(respBytes) > 0 {
		// JSON parse is best-effort — assertions that need it will fail
		// gracefully if the body isn't JSON.
		_ = json.Unmarshal(respBytes, &jsonBody)
	}

	for _, a := range step.Assertions {
		if a.Status != 0 && resp.StatusCode != a.Status {
			return fmt.Errorf("expected status %d, got %d", a.Status, resp.StatusCode)
		}
		if a.JSONPath != "" {
			val, err := jsonpath.Get(a.JSONPath, jsonBody)
			if a.Exists != nil {
				present := err == nil && val != nil
				if present != *a.Exists {
					return fmt.Errorf("jsonpath %s exists=%v, expected %v", a.JSONPath, present, *a.Exists)
				}
				continue
			}
			if err != nil {
				return fmt.Errorf("jsonpath %s: %w", a.JSONPath, err)
			}
			if a.Equals != nil && !jsonEqual(val, a.Equals) {
				return fmt.Errorf("jsonpath %s = %v, expected %v", a.JSONPath, val, a.Equals)
			}
			if a.Contains != "" {
				if s, ok := val.(string); !ok || !strings.Contains(s, a.Contains) {
					return fmt.Errorf("jsonpath %s does not contain %q (got %v)", a.JSONPath, a.Contains, val)
				}
			}
		} else if a.Contains != "" {
			if !strings.Contains(string(respBytes), a.Contains) {
				return fmt.Errorf("response body does not contain %q", a.Contains)
			}
		}
	}

	for name, path := range step.Extract {
		val, err := jsonpath.Get(path, jsonBody)
		if err != nil {
			return fmt.Errorf("extract %s via %s: %w", name, path, err)
		}
		vars[name] = fmt.Sprint(val)
	}
	return nil
}

func substitute(in string, vars map[string]string) (string, error) {
	if in == "" || !strings.Contains(in, "{{") {
		return in, nil
	}
	tmpl, err := template.New("ms").Option("missingkey=error").Parse(in)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func jsonEqual(a, b any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return bytes.Equal(ja, jb)
}

func displayName(step models.MultistepStep, idx int) string {
	if step.Name != "" {
		return step.Name
	}
	return fmt.Sprintf("step-%d", idx+1)
}
