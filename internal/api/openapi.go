package api

import (
	"net/http"
)

// openapiSpec returns an OpenAPI 3.0.3 document as a Go value.
func openapiSpec() map[string]any {
	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "GFire",
			"description": "Standalone background job service — REST API",
			"version":     "0.6.1",
		},
		"servers": []map[string]any{
			{"url": "http://localhost:8080", "description": "Local development"},
		},
		"paths": map[string]any{
			"/healthz": map[string]any{
				"get": map[string]any{
					"summary":     "Health check",
					"operationId": "healthz",
					"responses": map[string]any{
						"200": map[string]any{"description": "OK"},
					},
				},
			},
			"/readyz": map[string]any{
				"get": map[string]any{
					"summary":     "Readiness probe (storage reachable)",
					"operationId": "readyz",
					"responses": map[string]any{
						"200": map[string]any{"description": "Storage reachable"},
						"503": map[string]any{"description": "Storage unreachable"},
					},
				},
			},
			"/v1/jobs": map[string]any{
				"get": map[string]any{
					"summary":     "List jobs",
					"operationId": "listJobs",
					"parameters": []map[string]any{
						{"name": "state", "in": "query", "schema": map[string]any{"type": "string"}},
						{"name": "limit", "in": "query", "schema": map[string]any{"type": "integer", "default": 50}},
						{"name": "offset", "in": "query", "schema": map[string]any{"type": "integer", "default": 0}},
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Paginated job list"},
					},
				},
			},
			"/v1/jobs/enqueue": map[string]any{
				"post": map[string]any{
					"summary":     "Enqueue a job",
					"operationId": "enqueueJob",
					"parameters": []map[string]any{
						{"name": "Idempotency-Key", "in": "header", "schema": map[string]any{"type": "string"}, "description": "Client retry deduplication key"},
					},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/EnqueueRequest"},
							},
						},
					},
					"responses": map[string]any{
						"201": map[string]any{"description": "Job enqueued"},
						"400": map[string]any{"description": "Invalid request"},
					},
				},
			},
			"/v1/jobs/enqueue/batch": map[string]any{
				"post": map[string]any{
					"summary":     "Batch enqueue jobs (partial acceptance)",
					"operationId": "batchEnqueue",
					"parameters": []map[string]any{
						{"name": "Idempotency-Key", "in": "header", "schema": map[string]any{"type": "string"}, "description": "Per-job key:index"},
					},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/BatchEnqueueRequest"},
							},
						},
					},
					"responses": map[string]any{
						"201": map[string]any{"description": "Jobs enqueued (partial acceptance possible)"},
					},
				},
			},
			"/v1/jobs/schedule": map[string]any{
				"post": map[string]any{
					"summary":     "Schedule a job for delayed execution",
					"operationId": "scheduleJob",
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/ScheduleRequest"},
							},
						},
					},
					"responses": map[string]any{
						"201": map[string]any{"description": "Job scheduled"},
					},
				},
			},
			"/v1/jobs/{id}": map[string]any{
				"get": map[string]any{
					"summary":     "Get job detail + state history",
					"operationId": "getJob",
					"parameters": []map[string]any{
						{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Job detail"},
						"404": map[string]any{"description": "Job not found"},
					},
				},
			},
			"/v1/jobs/{id}/requeue": map[string]any{
				"post": map[string]any{
					"summary":     "Requeue a failed job",
					"operationId": "requeueJob",
					"parameters": []map[string]any{
						{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Job requeued"},
						"409": map[string]any{"description": "Conflict (terminal state)"},
					},
				},
			},
			"/v1/jobs/{id}/cancel": map[string]any{
				"post": map[string]any{
					"summary":     "Cancel an in-flight job",
					"operationId": "cancelJob",
					"parameters": []map[string]any{
						{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Job cancelled"},
					},
				},
			},
			"/v1/jobs/{id}/delete": map[string]any{
				"post": map[string]any{
					"summary":     "Soft-delete a job",
					"operationId": "deleteJob",
					"parameters": []map[string]any{
						{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Job deleted"},
						"409": map[string]any{"description": "Conflict (irreversible state)"},
					},
				},
			},
			"/v1/jobs/{id}/continue": map[string]any{
				"post": map[string]any{
					"summary":     "Create a continuation (child job on terminal state)",
					"operationId": "continueJob",
					"parameters": []map[string]any{
						{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
					},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/ContinueRequest"},
							},
						},
					},
					"responses": map[string]any{
						"201": map[string]any{"description": "Continuation registered"},
					},
				},
			},
			"/v1/queues": map[string]any{
				"get": map[string]any{
					"summary":     "List queues + depth",
					"operationId": "listQueues",
					"responses": map[string]any{
						"200": map[string]any{"description": "Queue list"},
					},
				},
			},
			"/v1/queues/{name}": map[string]any{
				"get": map[string]any{
					"summary":     "Get queue detail + stats",
					"operationId": "getQueue",
					"parameters": []map[string]any{
						{"name": "name", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Queue detail"},
					},
				},
			},
			"/v1/servers": map[string]any{
				"get": map[string]any{
					"summary":     "List registered servers",
					"operationId": "listServers",
					"responses": map[string]any{
						"200": map[string]any{"description": "Server list"},
					},
				},
			},
			"/v1/recurring": map[string]any{
				"get": map[string]any{
					"summary":     "List recurring job definitions",
					"operationId": "listRecurring",
					"responses": map[string]any{
						"200": map[string]any{"description": "Recurring job list"},
					},
				},
				"post": map[string]any{
					"summary":     "Create a recurring job",
					"operationId": "createRecurring",
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/RecurringRequest"},
							},
						},
					},
					"responses": map[string]any{
						"201": map[string]any{"description": "Recurring job created"},
					},
				},
			},
			"/v1/recurring/{id}": map[string]any{
				"delete": map[string]any{
					"summary":     "Remove a recurring job definition",
					"operationId": "deleteRecurring",
					"parameters": []map[string]any{
						{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Removed"},
					},
				},
			},
			"/v1/recurring/{id}/trigger": map[string]any{
				"post": map[string]any{
					"summary":     "Trigger a recurring job immediately",
					"operationId": "triggerRecurring",
					"parameters": []map[string]any{
						{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
					},
					"responses": map[string]any{
						"201": map[string]any{"description": "Job enqueued from trigger"},
					},
				},
			},
		},
		"components": map[string]any{
			"schemas": map[string]any{
				"EnqueueRequest": map[string]any{
					"type":     "object",
					"required": []string{"name"},
					"properties": map[string]any{
						"name":      map[string]any{"type": "string", "description": "Job handler name"},
						"args":      map[string]any{"type": "object", "description": "Job arguments (JSON)"},
						"queue":     map[string]any{"type": "string", "default": "default"},
						"retry_max": map[string]any{"type": "integer", "default": 10},
						"timeout":   map[string]any{"type": "string", "description": "Duration (e.g. 5m, 30s)"},
					},
				},
				"BatchEnqueueRequest": map[string]any{
					"type":     "object",
					"required": []string{"jobs"},
					"properties": map[string]any{
						"jobs": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/EnqueueRequest"},
						},
					},
				},
				"ScheduleRequest": map[string]any{
					"type":     "object",
					"required": []string{"name", "enqueue_at"},
					"properties": map[string]any{
						"name":       map[string]any{"type": "string"},
						"args":       map[string]any{"type": "object"},
						"queue":      map[string]any{"type": "string", "default": "default"},
						"retry_max":  map[string]any{"type": "integer"},
						"timeout":    map[string]any{"type": "string"},
						"enqueue_at": map[string]any{"type": "string", "format": "date-time", "description": "RFC 3339 timestamp"},
					},
				},
				"ContinueRequest": map[string]any{
					"type":     "object",
					"required": []string{"child_name"},
					"properties": map[string]any{
						"child_name":  map[string]any{"type": "string"},
						"child_args":  map[string]any{"type": "object"},
						"child_queue": map[string]any{"type": "string", "default": "default"},
						"condition":   map[string]any{"type": "string", "enum": []string{"OnSucceeded", "OnFailed", "OnAny"}, "default": "OnSucceeded"},
					},
				},
				"RecurringRequest": map[string]any{
					"type":     "object",
					"required": []string{"id", "job_name", "cron_expr"},
					"properties": map[string]any{
						"id":        map[string]any{"type": "string", "description": "Unique definition ID"},
						"job_name":  map[string]any{"type": "string"},
						"args":      map[string]any{"type": "object"},
						"queue":     map[string]any{"type": "string", "default": "default"},
						"cron_expr": map[string]any{"type": "string", "description": "Cron expression (seconds granularity)"},
						"enabled":   map[string]any{"type": "boolean", "default": true},
					},
				},
			},
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{
					"type":   "http",
					"scheme": "bearer",
				},
			},
		},
	}
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, openapiSpec())
}
