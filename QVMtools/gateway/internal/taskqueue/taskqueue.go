package taskqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"kvm_console_gateway/internal/model"
)

// TaskFunc 任务执行函数类型。
type TaskFunc func(ctx context.Context, task *model.Task, progress func(int, string)) (string, error)

var (
	handlers   = make(map[string]TaskFunc)
	handlersMu sync.RWMutex

	taskStore   = make(map[uint]*model.Task)
	taskStoreMu sync.RWMutex
	taskIDSeq   uint64
	taskChan    = make(chan uint, 100)
)

// RegisterHandler 注册任务处理器。
func RegisterHandler(taskType string, handler TaskFunc) {
	handlersMu.Lock()
	defer handlersMu.Unlock()
	handlers[taskType] = handler
	log.Printf("[taskqueue] 注册处理器: %s", taskType)
}

// Start 启动 worker 消费者。
func Start(workerCount int) {
	for i := 0; i < workerCount; i++ {
		go worker(i)
	}
	log.Printf("[taskqueue] 已启动 %d workers", workerCount)
}

// Submit 提交任务（内存模式，无 SQLite 持久化）。
func Submit(taskType, params, createdBy string) (*model.Task, error) {
	task := &model.Task{
		ID:        uint(atomic.AddUint64(&taskIDSeq, 1)),
		Type:      taskType,
		Status:    "pending",
		Params:    params,
		CreatedBy: createdBy,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	taskStoreMu.Lock()
	taskStore[task.ID] = task
	taskStoreMu.Unlock()

	select {
	case taskChan <- task.ID:
	default:
		task.Status = "failed"
		task.Message = "任务队列已满"
		task.UpdatedAt = time.Now()
		return task, fmt.Errorf("任务队列已满")
	}
	log.Printf("[taskqueue] 任务已提交: id=%d type=%s", task.ID, taskType)
	return task, nil
}

// SubmitWithStruct 提交任务（结构体参数自动序列化为 JSON）。
func SubmitWithStruct(taskType string, params interface{}, createdBy string) (*model.Task, error) {
	b, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return Submit(taskType, string(b), createdBy)
}

func worker(id int) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[taskqueue] worker panic: worker=%d recover=%v", id, r)
		}
	}()
	for taskID := range taskChan {
		processTask(id, taskID)
	}
}

func processTask(workerID int, taskID uint) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[taskqueue] PANIC: id=%d recover=%v", taskID, r)
			taskStoreMu.Lock()
			if task, ok := taskStore[taskID]; ok {
				task.Status = "failed"
				task.Message = fmt.Sprintf("panic: %v", r)
				task.UpdatedAt = time.Now()
			}
			taskStoreMu.Unlock()
		}
	}()

	log.Printf("[taskqueue] processTask start: worker=%d id=%d", workerID, taskID)

	taskStoreMu.RLock()
	task, ok := taskStore[taskID]
	taskStoreMu.RUnlock()
	if !ok {
		return
	}

	handlersMu.RLock()
	handler, exists := handlers[task.Type]
	handlersMu.RUnlock()
	if !exists {
		taskStoreMu.Lock()
		task.Status = "failed"
		task.Message = "未知任务类型: " + task.Type
		task.UpdatedAt = time.Now()
		taskStoreMu.Unlock()
		return
	}

	taskStoreMu.Lock()
	task.Status = "running"
	task.UpdatedAt = time.Now()
	taskStoreMu.Unlock()

	ctx := context.Background()
	progressFn := func(p int, msg string) {
		taskStoreMu.Lock()
		task.Progress = p
		task.Message = msg
		task.UpdatedAt = time.Now()
		taskStoreMu.Unlock()
	}

	result, err := handler(ctx, task, progressFn)
	taskStoreMu.Lock()
	if err != nil {
		task.Status = "failed"
		task.Message = err.Error()
		log.Printf("[taskqueue] 任务失败: id=%d err=%v", taskID, err)
	} else {
		task.Status = "done"
		task.Result = result
		task.Progress = 100
	}
	task.UpdatedAt = time.Now()
	taskStoreMu.Unlock()
	log.Printf("[taskqueue] 任务完成: id=%d status=%s", taskID, task.Status)
}
