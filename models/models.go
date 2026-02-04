package models

import "os"

type ConversionRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type Job struct {
	ID           string
	InputPath    string
	OutputPath   string
	FromFormat   string
	ToFormat     string
	Options      map[string]interface{}
	ResultChan   chan JobResult
	TempDir      string
}

type JobResult struct {
	Success bool
	Error   error
	Path    string
}

func (j *Job) Cleanup() {
	if j.TempDir != "" {
		os.RemoveAll(j.TempDir)
	}
}
