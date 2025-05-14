package reporter

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"com.example/pod/model"
	"com.example/pod/ws"
)

// ErrorReporter 错误报告工具
type ErrorReporter struct {
	wsManager *ws.Manager
}

// NewErrorReporter 创建错误报告工具
func NewErrorReporter(wsManager *ws.Manager) *ErrorReporter {
	return &ErrorReporter{
		wsManager: wsManager,
	}
}

// ReportError 向服务器上报错误
func (r *ErrorReporter) ReportError(source string, errMsg string, taskID string) error {
	if !r.wsManager.IsConnected() {
		log.Printf("无法上报错误，WebSocket连接未建立")
		return fmt.Errorf("WebSocket连接未建立")
	}

	errorData := model.ErrorData{
		Source:    source,
		Message:   errMsg,
		Timestamp: time.Now().Unix(),
		TaskID:    taskID,
	}

	data, err := json.Marshal(errorData)
	if err != nil {
		log.Printf("错误数据序列化失败: %v", err)
		return err
	}

	message := map[string]interface{}{
		"type": model.ErrorReport,
		"data": json.RawMessage(data),
	}

	if err := r.wsManager.SendTextMessage(message); err != nil {
		log.Printf("发送错误上报消息失败: %v", err)
		return err
	}

	log.Printf("已上报错误: %s - %s", source, errMsg)
	return nil
}

// ExtractTaskID 从消息数据中提取任务ID
func ExtractTaskID(data interface{}) string {
	if data == nil {
		return ""
	}

	// 尝试将data转换为map并提取taskID
	if m, ok := (data).(map[string]interface{}); ok {
		if taskID, exists := m["task_id"]; exists {
			if taskIDStr, ok := taskID.(string); ok {
				return taskIDStr
			}
		}
	}

	return ""
}
