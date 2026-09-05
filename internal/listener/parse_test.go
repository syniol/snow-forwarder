package listener

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

// setEnv sets some test envars
func setEnv() {

	os.Setenv("TABLE_NAME", "foo")
	os.Setenv("ISSUE_ID_FIELD", "issue.key")
	os.Setenv("SUMMARY_FIELD", "issue.fields.summary")
	os.Setenv("STATUS_FIELD", "issue.fields.status.name")
	os.Setenv("DESCRIPTION_FIELD", "issue.fields.description")
	os.Setenv("START_TIME_FIELD", "issue.fields.customfield_10109")
	os.Setenv("FINISH_TIME_FIELD", "issue.fields.customfield_10110")
}

// getMsg gets some test input
func getMsg(p int) (string, error) {

	body, err := ioutil.ReadFile("payloads.json")
	if err != nil {
		return "", err
	}

	path := fmt.Sprintf("cases.%v", p)
	res := gjson.GetManyBytes(body, path)

	return res[0].Raw, nil
}

func TestParseHandler(t *testing.T) {

	tt := []struct {
		name         string
		input        int
		supplierRef  string
		status       string
		title        string
		description  string
		starts       string
		ends         string
		recordErr    error
		expectedCode int
		errResp      string
	}{
		{
			name:         "good",
			input:        0,
			supplierRef:  "abc-1",
			status:       "scheduled",
			title:        "foo change",
			description:  "\nFor the most up-to-date info, visit /abc-1\nlorem impsum",
			starts:       "2020-09-01 18:30:00",
			ends:         "2020-09-01 19:30:00",
			expectedCode: http.StatusOK,
		},
		{
			name:         "missing",
			input:        1,
			expectedCode: http.StatusBadRequest,
			errResp:      "invalid request payload",
		},
		{
			name:         "time",
			input:        2,
			expectedCode: http.StatusBadRequest,
			errResp:      "invalid request payload",
		},
		{
			name:         "recorder error",
			input:        0,
			recordErr:    errors.New("dynamo write failed"),
			expectedCode: http.StatusInternalServerError,
			errResp:      "internal server error",
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {

			setEnv()

			var recorded *Record
			recordCalls := 0

			srv := &Server{
				Record: func(r *Record) error {
					recordCalls++
					recCopy := *r
					recorded = &recCopy
					return tc.recordErr
				},
			}

			// create inbound payload
			m, err := getMsg(tc.input)
			if err != nil {
				t.Fatalf("could not get message: %v", err)
			}

			rawM := json.RawMessage(m)
			p, err := json.Marshal(rawM)
			if err != nil {
				t.Fatalf("could not make incoming payload: %v", err)
			}
			pld := bytes.NewReader(p)

			// create inbound request
			r, err := http.NewRequest("POST", "/", pld)
			if err != nil {
				t.Fatalf("could not make incoming request: %v", err)
			}

			// create response recorder
			rr := httptest.NewRecorder()

			srv.ParseHandler(rr, r)

			res := rr.Result()
			defer res.Body.Close()

			b, err := ioutil.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("could not read response: %v", err)
			}

			if res.StatusCode != tc.expectedCode {
				t.Errorf("expected status %v, got %v", tc.expectedCode, res.StatusCode)
			}

			if tc.errResp == "" {
				if recordCalls != 1 {
					t.Errorf("expected recorder to be called once, got %d calls", recordCalls)
				}
				if recorded == nil {
					t.Fatal("expected recorded record to be non-nil")
				}
				if recorded.SupplierRef != tc.supplierRef {
					t.Errorf("expected supplierRef %v, got %v", tc.supplierRef, recorded.SupplierRef)
				}
				if recorded.Status != tc.status {
					t.Errorf("expected status %v, got %v", tc.status, recorded.Status)
				}
				if recorded.Title != tc.title {
					t.Errorf("expected title %v, got %v", tc.title, recorded.Title)
				}
				if recorded.Description != tc.description {
					t.Errorf("expected description %v, got %v", tc.description, recorded.Description)
				}
				if recorded.Starts != tc.starts {
					t.Errorf("expected starts %v, got %v", tc.starts, recorded.Starts)
				}
				if recorded.Ends != tc.ends {
					t.Errorf("expected ends %v, got %v", tc.ends, recorded.Ends)
				}
			} else {
				if tc.recordErr == nil && recordCalls != 0 {
					t.Errorf("expected recorder not to be called on parser error, got %d calls", recordCalls)
				}
				if msg := string(bytes.TrimSpace(b)); !strings.Contains(msg, tc.errResp) {
					t.Errorf("expected sanitized error %q, got: %q", tc.errResp, msg)
				}
			}
		})
	}
}

func TestParseHandler_PayloadSizeLimit(t *testing.T) {
	setEnv()

	recordCalls := 0
	srv := &Server{
		Record: func(r *Record) error {
			recordCalls++
			return nil
		},
	}

	// Create payload larger than 1 MiB
	largeData := bytes.Repeat([]byte("a"), (1<<20)+1024)
	r, err := http.NewRequest("POST", "/", bytes.NewReader(largeData))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	srv.ParseHandler(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for oversized payload, got %d", rr.Code)
	}
	if recordCalls != 0 {
		t.Errorf("expected recorder not to be called on oversized payload, got %d calls", recordCalls)
	}
	body := strings.TrimSpace(rr.Body.String())
	if !strings.Contains(body, "invalid request payload") {
		t.Errorf("expected sanitized error message, got %q", body)
	}
}

func TestParseHandler_RequestIsolation(t *testing.T) {
	setEnv()

	var recordedRecords []*Record
	srv := &Server{
		Record: func(r *Record) error {
			recCopy := *r
			recordedRecords = append(recordedRecords, &recCopy)
			return nil
		},
	}

	// 1. Send good request (case 0)
	m0, err := getMsg(0)
	if err != nil {
		t.Fatalf("getMsg(0) failed: %v", err)
	}
	r0, _ := http.NewRequest("POST", "/", strings.NewReader(m0))
	w0 := httptest.NewRecorder()
	srv.ParseHandler(w0, r0)
	if w0.Code != http.StatusOK {
		t.Fatalf("request 1 expected status 200, got %d", w0.Code)
	}
	if len(recordedRecords) != 1 {
		t.Fatalf("expected 1 record call, got %d", len(recordedRecords))
	}
	if recordedRecords[0].SupplierRef != "abc-1" {
		t.Errorf("expected abc-1, got %v", recordedRecords[0].SupplierRef)
	}

	// 2. Send bad request (case 1) - should not invoke recorder or corrupt state
	m1, err := getMsg(1)
	if err != nil {
		t.Fatalf("getMsg(1) failed: %v", err)
	}
	r1, _ := http.NewRequest("POST", "/", strings.NewReader(m1))
	w1 := httptest.NewRecorder()
	srv.ParseHandler(w1, r1)
	if w1.Code != http.StatusBadRequest {
		t.Fatalf("request 2 expected status 400, got %d", w1.Code)
	}
	if len(recordedRecords) != 1 {
		t.Fatalf("expected still 1 record call after bad request, got %d", len(recordedRecords))
	}

	// 3. Send another good request with custom payload
	customPayload := `{
		"issue": {
			"key": "xyz-99",
			"fields": {
				"customfield_10109": "2020-09-01T17:30:00.000+0000",
				"customfield_10110": "2020-09-01T18:30:00.000+0000",
				"status": {
					"name": "completed"
				},
				"description": "isolated description",
				"summary": "isolated summary"
			}
		}
	}`
	r2, _ := http.NewRequest("POST", "/", strings.NewReader(customPayload))
	w2 := httptest.NewRecorder()
	srv.ParseHandler(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("request 3 expected status 200, got %d", w2.Code)
	}
	if len(recordedRecords) != 2 {
		t.Fatalf("expected 2 record calls, got %d", len(recordedRecords))
	}
	if recordedRecords[1].SupplierRef != "xyz-99" {
		t.Errorf("expected xyz-99, got %v", recordedRecords[1].SupplierRef)
	}
	if recordedRecords[1].Status != "completed" {
		t.Errorf("expected completed, got %v", recordedRecords[1].Status)
	}
	if recordedRecords[1].Title != "isolated summary" {
		t.Errorf("expected isolated summary, got %v", recordedRecords[1].Title)
	}
}

func TestNewServerAndHandler(t *testing.T) {
	s := NewServer()
	if s == nil {
		t.Fatal("expected NewServer to return non-nil Server")
	}
	if s.Record == nil {
		t.Error("expected NewServer to initialize default Record function")
	}

	h := Handler()
	if h == nil {
		t.Fatal("expected Handler to return non-nil http.Handler")
	}
}

