package model

// MessageType 消息类型
type MessageType string

const (
	Pong            MessageType = "pong"
	InitNode        MessageType = "init_node"
	UpdateProject   MessageType = "update_project"   // 全量更新项目
	UpdateResources MessageType = "update_resources" // 增量更新资源
	ErrorReport     MessageType = "error_report"     // 错误上报
	Sync            MessageType = "sync"             // 同步资源
)

// TextMessage WebSocket消息结构
type TextMessage struct {
	Type MessageType `json:"type"`
	Data interface{} `json:"data,omitempty"`
}

// ErrorData 定义错误上报的数据结构
type ErrorData struct {
	Source    string `json:"source"`    // 错误来源
	Message   string `json:"message"`   // 错误消息
	Timestamp int64  `json:"timestamp"` // 错误发生时间戳
	TaskID    string `json:"task_id"`   // 任务ID，如果有的话
}

// DownloadInfo 下载信息结构体
type DownloadInfo struct {
	FileID      string `json:"file_id"`      // 文件唯一标识
	FileName    string `json:"file_name"`    // 文件名
	FileSize    int64  `json:"file_size"`    // 文件大小
	ChunkSize   int64  `json:"chunk_size"`   // 分片大小
	TotalChunks int    `json:"total_chunks"` // 分片总数
	FileHash    string `json:"file_hash"`    // 文件哈希值
	DownloadUrl string `json:"download_url"` // 下载URL
}

// FileInfo 文件信息结构
type FileInfo struct {
	Name string
	Size int64
	Hash string // 可选的文件哈希，用于完整性校验
}
