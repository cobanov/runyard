package main

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestMLflow walks the calls mlflow's client makes (checked against mlflow 3.17: start_run, log_*, autolog with a
// logged model, artifacts), in its wire format: bare NaN in JSON, errors as {error_code, message}.
func TestMLflow(t *testing.T) {
	ts, key, _ := testServer(t)
	api := ts.URL + "/api/2.0/mlflow/"
	do := func(method, path, auth, body string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(method, path, strings.NewReader(body))
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		return res.StatusCode, string(b)
	}
	expect := func(code int, body string, wantCode int, want ...string) {
		t.Helper()
		if code != wantCode {
			t.Fatalf("got %d %s, want %d", code, body, wantCode)
		}
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Fatalf("%s lacks %s", body, w)
			}
		}
	}

	code, body := do("GET", api+"experiments/get-by-name?experiment_name=sft", "", "")
	expect(code, body, 404, `"error_code":"RESOURCE_DOES_NOT_EXIST"`)
	code, body = do("POST", api+"experiments/create", "", `{"name":"sft"}`)
	expect(code, body, 401, `"error_code":"UNAUTHENTICATED"`)
	code, body = do("POST", api+"experiments/create", key, `{"name":"sft"}`)
	expect(code, body, 200, `"experiment_id":"1"`)
	code, body = do("POST", api+"experiments/create", key, `{"name":"sft"}`)
	expect(code, body, 400, "RESOURCE_ALREADY_EXISTS")

	code, body = do("POST", api+"runs/create", key, `{"experiment_id":"1","run_name":"lr3e-4","start_time":1700000000000,
		"tags":[{"key":"mlflow.user","value":"me"},{"key":"mlflow.source.git.commit","value":"abc123"},{"key":"team","value":"vision"}]}`)
	expect(code, body, 200, `"run_name":"lr3e-4"`, `"status":"RUNNING"`, `"artifact_uri":"mlflow-artifacts:/1/`)
	id := body[strings.Index(body, `"run_id":"`)+10:][:36]

	code, body = do("POST", api+"runs/log-batch", key, `{"run_id":"`+id+`","metrics":[{"key":"loss","value":0.5,"timestamp":1700000001000,"step":1},
		{"key":"loss","value":NaN,"timestamp":1700000002000,"step":2},{"key":"lr","value":-Infinity,"timestamp":1700000002000,"step":2}],
		"params":[{"key":"batch","value":"32"}],"tags":[{"key":"stage","value":"sft"}]}`)
	expect(code, body, 200)
	code, body = do("POST", api+"runs/update", key, `{"run_id":"`+id+`","status":"FINISHED","end_time":1700000003000}`)
	expect(code, body, 200, `"status":"FINISHED"`, `"end_time"`)
	code, body = do("GET", api+"runs/get?run_id="+id, "", "")
	expect(code, body, 200, `{"key":"batch","value":"32"}`, `{"key":"team","value":"vision"}`, `{"key":"stage","value":"sft"}`, `"value":"NaN"`)
	code, body = do("GET", api+"metrics/get-history?run_id="+id+"&metric_key=loss", "", "")
	expect(code, body, 200, `"value":0.5,"timestamp":1700000001000,"step":1`)
	code, body = do("GET", ts.URL+"/api/v1/runs/"+id, "", "")
	expect(code, body, 200, `"git":{"commit":"abc123"}`, `"status":"finished"`)

	// Artifacts by path; a second upload to the same path replaces the first.
	arts := ts.URL + "/api/2.0/mlflow-artifacts/artifacts"
	for _, content := range []string{"v1", "v2"} {
		code, body = do("PUT", arts+"/1/"+id+"/artifacts/model/MLmodel", key, content)
		expect(code, body, 200)
	}
	code, body = do("PUT", arts+"/1/"+id+"/artifacts/weights.bin", key, "0000")
	expect(code, body, 200)
	code, body = do("GET", arts+"?path=1/"+id+"/artifacts", "", "")
	expect(code, body, 200, `{"path":"model","is_dir":true}`, `{"path":"weights.bin","is_dir":false,"file_size":4}`)
	code, body = do("GET", arts+"/1/"+id+"/artifacts/model/MLmodel", "", "")
	expect(code, body, 200, "v2")

	// A logged model belongs to its run; its files are artifacts under models/<id>/.
	code, body = do("POST", api+"logged-models", key, `{"experiment_id":"1","name":"model","source_run_id":"`+id+`"}`)
	expect(code, body, 200, `"status":"LOGGED_MODEL_PENDING"`)
	model := body[strings.Index(body, `"model_id":"`)+12:][:43]
	code, body = do("PUT", arts+"/1/models/"+model+"/artifacts/model.pkl", key, "pickle")
	expect(code, body, 200)
	code, body = do("PATCH", api+"logged-models/"+model, key, `{"model_id":"`+model+`","status":"LOGGED_MODEL_READY"}`)
	expect(code, body, 200, `"status":"LOGGED_MODEL_READY"`, `"source_run_id":"`+id)
	code, body = do("GET", arts+"/1/models/"+model+"/artifacts/model.pkl", "", "")
	expect(code, body, 200, "pickle")
	code, body = do("POST", api+"logged-models", key, `{"experiment_id":"1","name":"orphan"}`)
	expect(code, body, 400, "INVALID_PARAMETER_VALUE")

	code, body = do("POST", api+"runs/create", key, `{"run_name":"no-experiment"}`) // MLflow's default experiment "0"
	expect(code, body, 200)
	exp, _, _ := strings.Cut(body[strings.Index(body, `"experiment_id":"`)+17:], `"`)
	code, body = do("GET", api+"experiments/get?experiment_id="+exp, "", "")
	expect(code, body, 200, `"name":"default"`)
	code, body = do("POST", api+"runs/log-metric", key, `{"run_id":"00000000-0000-4000-8000-000000000000","key":"x","value":1,"timestamp":1,"step":0}`)
	expect(code, body, 404, "RESOURCE_DOES_NOT_EXIST")
	code, body = do("GET", api+"no/such/thing", "", "")
	expect(code, body, 404, "ENDPOINT_NOT_FOUND")
	if !bytes.Contains([]byte(body), []byte("GET /api/2.0/mlflow/no/such/thing")) {
		t.Fatal(body)
	}
}
