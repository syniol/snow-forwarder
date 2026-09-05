package listener

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/tidwall/gjson"
)

func checkVars(input string) error {

	vars := []string{"ISSUE_ID_FIELD", "STATUS_FIELD", "SUMMARY_FIELD",
		"DESCRIPTION_FIELD", "START_TIME_FIELD", "FINISH_TIME_FIELD"}

	for _, v := range vars {
		field, ok := os.LookupEnv(v)
		if !ok {
			return errors.New("missing environment variable")
		}
		value := gjson.Get(input, field)
		if !value.Exists() {
			return errors.New("missing value in payload")
		}
	}
	return nil
}

// ParseRequest gets some values from inbound paylpad
func (r *Record) ParseRequest(input string) error {

	err := checkVars(input)
	if err != nil {
		return err
	}

	tab, ok := os.LookupEnv("TABLE_NAME")
	if !ok {
		return errors.New("missing table name")
	}
	r.Table = tab
	r.Description = gjson.Get(input, os.Getenv("DESCRIPTION_FIELD")).Str
	r.SupplierRef = gjson.Get(input, os.Getenv("ISSUE_ID_FIELD")).Str
	r.Status = gjson.Get(input, os.Getenv("STATUS_FIELD")).Str
	r.Title = gjson.Get(input, os.Getenv("SUMMARY_FIELD")).Str

	// prefix description with link
	desc := "\nFor the most up-to-date info, visit " +
		os.Getenv("JSD_URL") + "/" + r.SupplierRef + "\n" + r.Description
	r.Description = desc

	log.Printf("processing JSD event: %v, status: %v\n", r.SupplierRef, r.Status)
	return nil
}

// ParseTime gets timestamps from inbound request and formats them for SNOW
func (r *Record) ParseTime(input string) error {

	err := checkVars(input)
	if err != nil {
		return err
	}

	r.Ends = gjson.Get(input, os.Getenv("FINISH_TIME_FIELD")).Str
	r.Starts = gjson.Get(input, os.Getenv("START_TIME_FIELD")).Str

	const (
		layout     = "2006-01-02T15:04:05.000+0000"
		layoutSNOW = "2006-01-02 15:04:05"
	)

	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		return err
	}

	ett, err := time.Parse(layout, r.Ends)
	if err != nil {
		return err
	}

	stt, err := time.Parse(layout, r.Starts)
	if err != nil {
		return err
	}

	r.Starts = stt.In(loc).Format(layoutSNOW)
	r.Ends = ett.In(loc).Format(layoutSNOW)

	return nil
}

// ParseHandler receives a payload from JSD
func ParseHandler(w http.ResponseWriter, req *http.Request) {

	// read incoming request body
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	input := buf.String()

	var r Record
	err = r.ParseRequest(input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err = r.ParseTime(input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err = record(&r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
