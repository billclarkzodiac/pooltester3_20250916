package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v2"
)

// JobAction represents a single action to perform on a device
type JobAction struct {
	Type         string                 `json:"type" yaml:"type"` // "send_message", "wait", "condition"
	DeviceID     string                 `json:"device_id" yaml:"device_id"`
	MessageType  string                 `json:"message_type" yaml:"message_type"`
	Parameters   map[string]interface{} `json:"parameters" yaml:"parameters"`
	WaitDuration string                 `json:"wait_duration" yaml:"wait_duration"` // e.g., "5s", "1m"
	Condition    *JobCondition          `json:"condition" yaml:"condition"`
	OnSuccess    []JobAction            `json:"on_success" yaml:"on_success"`
	OnFailure    []JobAction            `json:"on_failure" yaml:"on_failure"`
	Retry        *RetryConfig           `json:"retry" yaml:"retry"`
	Tags         []string               `json:"tags" yaml:"tags"`
}

// JobCondition represents a condition to check before proceeding
type JobCondition struct {
	Type      string      `json:"type" yaml:"type"` // "field_equals", "field_greater", "device_online", etc.
	DeviceID  string      `json:"device_id" yaml:"device_id"`
	FieldPath string      `json:"field_path" yaml:"field_path"` // e.g., "telemetry.temperature"
	Operator  string      `json:"operator" yaml:"operator"`     // "==", "!=", ">", "<", ">=", "<="
	Value     interface{} `json:"value" yaml:"value"`
	Timeout   string      `json:"timeout" yaml:"timeout"` // e.g., "30s"
}

// RetryConfig defines retry behavior for actions
type RetryConfig struct {
	MaxAttempts int    `json:"max_attempts" yaml:"max_attempts"`
	Interval    string `json:"interval" yaml:"interval"` // e.g., "1s", "5s"
	Backoff     string `json:"backoff" yaml:"backoff"`   // "linear", "exponential"
}

// Job represents a complete automation job
type Job struct {
	ID          string      `json:"id" yaml:"id"`
	Name        string      `json:"name" yaml:"name"`
	Description string      `json:"description" yaml:"description"`
	Schedule    *Schedule   `json:"schedule" yaml:"schedule"`
	Actions     []JobAction `json:"actions" yaml:"actions"`
	Enabled     bool        `json:"enabled" yaml:"enabled"`
	Tags        []string    `json:"tags" yaml:"tags"`
	CreatedAt   time.Time   `json:"created_at" yaml:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at" yaml:"updated_at"`
}

// Schedule defines when a job should run
type Schedule struct {
	Type     string `json:"type" yaml:"type"`         // "once", "interval", "cron"
	Interval string `json:"interval" yaml:"interval"` // e.g., "1h", "30m"
	Cron     string `json:"cron" yaml:"cron"`         // cron expression
	StartAt  string `json:"start_at" yaml:"start_at"` // ISO 8601 timestamp
}

// JobExecution represents a single execution instance of a job
type JobExecution struct {
	ID        string                 `json:"id"`
	JobID     string                 `json:"job_id"`
	StartTime time.Time              `json:"start_time"`
	EndTime   time.Time              `json:"end_time"`
	Status    string                 `json:"status"` // "running", "completed", "failed", "cancelled"
	Results   []ActionResult         `json:"results"`
	Error     string                 `json:"error,omitempty"`
	Context   map[string]interface{} `json:"context"` // Shared data between actions
}

// ActionResult represents the result of executing a single action
type ActionResult struct {
	ActionIndex   int                    `json:"action_index"`
	ActionType    string                 `json:"action_type"`
	StartTime     time.Time              `json:"start_time"`
	EndTime       time.Time              `json:"end_time"`
	Success       bool                   `json:"success"`
	Error         string                 `json:"error,omitempty"`
	Response      map[string]interface{} `json:"response,omitempty"`
	RetryAttempts int                    `json:"retry_attempts"`
}

// JobEngine manages and executes automation jobs
type JobEngine struct {
	jobs       map[string]*Job
	executions map[string]*JobExecution
	mutex      sync.RWMutex
	scheduler  *JobScheduler
	deviceComm *DeviceCommunicator
	logger     *DeviceLogger
	registry   *ProtobufCommandRegistry
	stopChan   chan struct{}
	running    bool
}

// NewJobEngine creates a new job automation engine
func NewJobEngine(deviceComm *DeviceCommunicator, logger *DeviceLogger, registry *ProtobufCommandRegistry) *JobEngine {
	engine := &JobEngine{
		jobs:       make(map[string]*Job),
		executions: make(map[string]*JobExecution),
		deviceComm: deviceComm,
		logger:     logger,
		registry:   registry,
		stopChan:   make(chan struct{}),
	}

	engine.scheduler = NewJobScheduler(engine)
	return engine
}

// LoadJobsFromFile loads jobs from a JSON or YAML file
func (je *JobEngine) LoadJobsFromFile(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("error reading job file: %v", err)
	}

	var jobs []Job

	// Try JSON first, then YAML
	if err := json.Unmarshal(data, &jobs); err != nil {
		if err := yaml.Unmarshal(data, &jobs); err != nil {
			return fmt.Errorf("error parsing job file (tried JSON and YAML): %v", err)
		}
	}

	// Load jobs into engine
	for _, job := range jobs {
		if err := je.AddJob(&job); err != nil {
			return fmt.Errorf("error adding job %s: %v", job.ID, err)
		}
	}

	return nil
}

// AddJob adds a new job to the engine
func (je *JobEngine) AddJob(job *Job) error {
	je.mutex.Lock()
	defer je.mutex.Unlock()

	// Validate job
	if err := je.validateJob(job); err != nil {
		return err
	}

	// Set timestamps
	now := time.Now()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now

	je.jobs[job.ID] = job

	// Schedule if enabled
	if job.Enabled && job.Schedule != nil {
		je.scheduler.ScheduleJob(job)
	}

	return nil
}

// validateJob validates a job definition
func (je *JobEngine) validateJob(job *Job) error {
	if job.ID == "" {
		return fmt.Errorf("job ID is required")
	}

	if job.Name == "" {
		return fmt.Errorf("job name is required")
	}

	if len(job.Actions) == 0 {
		return fmt.Errorf("job must have at least one action")
	}

	// Validate actions
	for i, action := range job.Actions {
		if err := je.validateAction(&action); err != nil {
			return fmt.Errorf("action %d: %v", i, err)
		}
	}

	return nil
}

// validateAction validates a single action
func (je *JobEngine) validateAction(action *JobAction) error {
	switch action.Type {
	case "send_message":
		if action.DeviceID == "" {
			return fmt.Errorf("device_id is required for send_message action")
		}
		if action.MessageType == "" {
			return fmt.Errorf("message_type is required for send_message action")
		}
	case "wait":
		if action.WaitDuration == "" {
			return fmt.Errorf("wait_duration is required for wait action")
		}
		if _, err := time.ParseDuration(action.WaitDuration); err != nil {
			return fmt.Errorf("invalid wait_duration: %v", err)
		}
	case "condition":
		if action.Condition == nil {
			return fmt.Errorf("condition is required for condition action")
		}
	default:
		return fmt.Errorf("unknown action type: %s", action.Type)
	}

	return nil
}

// ExecuteJob executes a job immediately
func (je *JobEngine) ExecuteJob(jobID string) (*JobExecution, error) {
	je.mutex.RLock()
	job, exists := je.jobs[jobID]
	je.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("job %s not found", jobID)
	}

	if !job.Enabled {
		return nil, fmt.Errorf("job %s is disabled", jobID)
	}

	return je.executeJobInternal(job)
}

// executeJobInternal performs the actual job execution
func (je *JobEngine) executeJobInternal(job *Job) (*JobExecution, error) {
	execution := &JobExecution{
		ID:        fmt.Sprintf("exec_%d", time.Now().UnixNano()),
		JobID:     job.ID,
		StartTime: time.Now(),
		Status:    "running",
		Results:   make([]ActionResult, 0),
		Context:   make(map[string]interface{}),
	}

	// Store execution
	je.mutex.Lock()
	je.executions[execution.ID] = execution
	je.mutex.Unlock()

	// Execute actions in sequence
	for i, action := range job.Actions {
		result := je.executeAction(&action, i, execution)
		execution.Results = append(execution.Results, result)

		if !result.Success {
			execution.Status = "failed"
			execution.Error = result.Error
			break
		}
	}

	execution.EndTime = time.Now()
	if execution.Status == "running" {
		execution.Status = "completed"
	}

	return execution, nil
}

// executeAction executes a single action
func (je *JobEngine) executeAction(action *JobAction, index int, execution *JobExecution) ActionResult {
	result := ActionResult{
		ActionIndex: index,
		ActionType:  action.Type,
		StartTime:   time.Now(),
	}

	var err error
	maxAttempts := 1
	interval := time.Second

	if action.Retry != nil {
		maxAttempts = action.Retry.MaxAttempts
		if action.Retry.Interval != "" {
			if d, parseErr := time.ParseDuration(action.Retry.Interval); parseErr == nil {
				interval = d
			}
		}
	}

	// Execute with retry logic
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result.RetryAttempts = attempt - 1

		switch action.Type {
		case "send_message":
			err = je.executeSendMessage(action, execution, &result)
		case "wait":
			err = je.executeWait(action)
		case "condition":
			err = je.executeCondition(action, execution)
		default:
			err = fmt.Errorf("unknown action type: %s", action.Type)
		}

		if err == nil {
			result.Success = true
			break
		}

		if attempt < maxAttempts {
			time.Sleep(interval)
			// Apply backoff if configured
			if action.Retry != nil && action.Retry.Backoff == "exponential" {
				interval *= 2
			}
		}
	}

	result.EndTime = time.Now()
	if err != nil {
		result.Error = err.Error()
	}

	return result
}

// executeSendMessage executes a send_message action
func (je *JobEngine) executeSendMessage(action *JobAction, execution *JobExecution, result *ActionResult) error {
	// Create protobuf message
	// // 	msg, err := je.registry.CreateMessage(action.MessageType)
	//	if err != nil {
	//		return fmt.Errorf("failed to create message: %v", err)
	//	}

	// Populate message fields from parameters
	//	if err := je.populateMessage(msg, action.Parameters); err != nil {
	//		return fmt.Errorf("failed to populate message: %v", err)
	//	}

	// Send message and get response (placeholder implementation)
	// response, err := je.deviceComm.SendMessage(action.DeviceID, msg)
	//	response := msg // For now, just echo the message back
	//	err = nil
	//	if err != nil {
	//		return fmt.Errorf("failed to send message: %v", err)
	//	}

	// Store response in result and execution context
	// 	if response != nil {
	// 		result.Response = je.protoToMap(response)
	// 		execution.Context[fmt.Sprintf("response_%d", result.ActionIndex)] = result.Response
	// 	}

	// TODO: Implement actual message sending when protobuf issues are resolved
	fmt.Printf("📤 Would send message: %s to device: %s\n", action.MessageType, action.DeviceID)

	// Store a placeholder response
	result.Response = map[string]interface{}{
		"message_type": action.MessageType,
		"device_id":    action.DeviceID,
		"status":       "simulated",
	}
	return nil
}

// executeWait executes a wait action
func (je *JobEngine) executeWait(action *JobAction) error {
	duration, err := time.ParseDuration(action.WaitDuration)
	if err != nil {
		return err
	}

	time.Sleep(duration)
	return nil
}

// executeCondition executes a condition action
func (je *JobEngine) executeCondition(action *JobAction, execution *JobExecution) error {
	// Implementation for condition evaluation
	// This would check device state, field values, etc.
	return nil
}

// populateMessage populates a protobuf message with parameters
func (je *JobEngine) populateMessage(msg interface{}, params map[string]interface{}) error {
	// Implementation for setting protobuf message fields from parameters
	// This would use reflection to set field values
	return nil
}

// protoToMap converts a protobuf message to a map
func (je *JobEngine) protoToMap(msg interface{}) map[string]interface{} {
	// Implementation for converting protobuf to map
	return make(map[string]interface{})
}

// GetJobs returns all jobs
func (je *JobEngine) GetJobs() map[string]*Job {
	je.mutex.RLock()
	defer je.mutex.RUnlock()

	result := make(map[string]*Job)
	for k, v := range je.jobs {
		result[k] = v
	}
	return result
}

// GetExecutions returns job executions with optional filtering
func (je *JobEngine) GetExecutions(jobID string, limit int) []*JobExecution {
	je.mutex.RLock()
	defer je.mutex.RUnlock()

	var executions []*JobExecution
	for _, exec := range je.executions {
		if jobID == "" || exec.JobID == jobID {
			executions = append(executions, exec)
		}
	}

	// Sort by start time (newest first) and limit
	// Implementation for sorting and limiting would go here

	return executions
}

// JobScheduler handles scheduling of jobs
type JobScheduler struct {
	engine *JobEngine
}

// NewJobScheduler creates a new job scheduler
func NewJobScheduler(engine *JobEngine) *JobScheduler {
	return &JobScheduler{engine: engine}
}

// ScheduleJob schedules a job based on its schedule configuration
func (js *JobScheduler) ScheduleJob(job *Job) {
	// Implementation for job scheduling
	// This would handle cron expressions, intervals, etc.
}

// DeviceCommunicator interface for sending messages to devices
type DeviceCommunicator interface {
	SendMessage(deviceID string, message interface{}) (interface{}, error)
}
