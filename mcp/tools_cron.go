package mcp

import (
	"encoding/json"
	"fmt"
	"time"

	"ergo.services/ergo/gen"
)

func registerCronTools(r *toolRegistry) {
	r.register(ToolDefinition{
		Name:        "cron_info",
		Description: "Returns cron scheduler information: next run time, queued jobs count, all job details.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {},
			"additionalProperties": false
		}`),
		handler: toolCronInfo,
	})

	r.register(ToolDefinition{
		Name:        "cron_job",
		Description: "Returns detailed information about a specific cron job: spec, location, last run, last error.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {
					"type": "string",
					"description": "Job name"
				}
			},
			"required": ["name"]
		}`),
		handler: toolCronJob,
	})

	r.register(ToolDefinition{
		Name:        "cron_schedule",
		Description: "Returns all jobs planned to run within the given time period from now.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"duration_seconds": {
					"type": "integer",
					"description": "Time period in seconds to look ahead (default: 3600)"
				}
			}
		}`),
		handler: toolCronSchedule,
	})
}

func toolCronInfo(w gen.Process, params json.RawMessage) (any, error) {
	cron := w.Node().Cron()
	info := cron.Info()
	text, err := marshalResult(info)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

type cronJobParams struct {
	Name string `json:"name"`
}

func toolCronJob(w gen.Process, params json.RawMessage) (any, error) {
	var p cronJobParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	cron := w.Node().Cron()
	info, err := cron.JobInfo(gen.Atom(p.Name))
	if err != nil {
		return nil, fmt.Errorf("cron_job: %w", err)
	}
	text, err := marshalResult(info)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

type cronScheduleParams struct {
	DurationSeconds int `json:"duration_seconds"`
}

func toolCronSchedule(w gen.Process, params json.RawMessage) (any, error) {
	var p cronScheduleParams
	if len(params) > 0 {
		json.Unmarshal(params, &p)
	}
	if p.DurationSeconds < 1 {
		p.DurationSeconds = 3600
	}

	cron := w.Node().Cron()
	schedule := cron.Schedule(time.Now(), time.Duration(p.DurationSeconds)*time.Second)
	text, err := marshalResult(schedule)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}
