package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"

	"com.example/pod/config"
	"com.example/pod/downloader"
	"com.example/pod/model"
	"com.example/pod/reporter"
)

// MessageHandler 处理WebSocket消息
type MessageHandler struct {
	downloader    *downloader.FileDownloader
	errorReporter *reporter.ErrorReporter
	config        *config.Config
}

// NewMessageHandler 创建消息处理器
func NewMessageHandler(downloader *downloader.FileDownloader, errorReporter *reporter.ErrorReporter, config *config.Config) *MessageHandler {
	return &MessageHandler{
		downloader:    downloader,
		errorReporter: errorReporter,
		config:        config,
	}
}

// parseMessage 解析消息并提取taskID
func (h *MessageHandler) parseMessage(message []byte) (model.TextMessage, string, error) {
	var textMessage model.TextMessage
	if err := json.Unmarshal(message, &textMessage); err != nil {
		log.Printf("解析消息失败: %v", err)
		h.errorReporter.ReportError("message_parsing", fmt.Sprintf("解析消息失败: %v", err), "")
		return textMessage, "", err
	}

	// 提取任务ID用于错误上报
	taskID := reporter.ExtractTaskID(textMessage.Data)
	return textMessage, taskID, nil
}

// reportError 报告错误并返回包装后的错误
func (h *MessageHandler) reportError(msgType model.MessageType, format string, taskID string, err error) error {
	errMsg := fmt.Errorf(format, err)
	h.errorReporter.ReportError(string(msgType), errMsg.Error(), taskID)
	return errMsg
}

// HandleInitNode 处理初始化节点消息
func (h *MessageHandler) HandleInitNode(message []byte) error {
	// 解析消息获取可能的任务ID，用于错误报告
	_, taskID, err := h.parseMessage(message)
	if err != nil {
		return err
	}

	// 检查 /opt/docker-apps 是否存在
	log.Printf("检查 /opt/docker-apps 是否存在")
	if _, err := os.Stat("/opt/docker-apps"); os.IsNotExist(err) {
		// 创建 /opt/docker-apps 目录
		err := os.MkdirAll("/opt/docker-apps", 0755)
		if err != nil {
			return h.reportError(model.InitNode, "创建 /opt/docker-apps 目录失败: %v", taskID, err)
		}
	}

	// 检查 /opt/docker-apps 是否为空，不为空，则清空
	if _, err := os.Stat("/opt/docker-apps"); err == nil {
		err := os.RemoveAll("/opt/docker-apps")
		if err != nil {
			return h.reportError(model.InitNode, "清空 /opt/docker-apps 目录失败: %v", taskID, err)
		}
	}

	log.Printf("下载压缩文件")
	// 下载压缩文件
	if err := h.downloader.DownloadFile("stub.tar", "/opt/docker-apps"); err != nil {
		return h.reportError(model.InitNode, "下载压缩文件失败: %v", taskID, err)
	}

	log.Printf("下载 compose.yaml 文件")
	if err := h.downloader.DownloadFile(fmt.Sprintf("%s.yaml", h.config.NodeUID), "/opt/docker-apps", "compose.yaml"); err != nil {
		return h.reportError(model.InitNode, "下载 compose.yaml 文件失败: %v", taskID, err)
	}

	log.Printf("下载初始化脚本")
	// 下载初始化脚本
	if err := h.downloader.DownloadFile("setup", "/opt/docker-apps"); err != nil {
		return h.reportError(model.InitNode, "下载初始化脚本失败: %v", taskID, err)
	}

	// 赋予可执行权限
	err = os.Chmod("/opt/docker-apps/setup", 0755)
	if err != nil {
		return h.reportError(model.InitNode, "赋予可执行权限失败: %v", taskID, err)
	}

	// 调用 setup 脚本，执行初始化
	setupScript := "/opt/docker-apps/setup"
	cmd := exec.Command(setupScript)
	cmd.Dir = "/opt/docker-apps"
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return h.reportError(model.InitNode, "执行初始化脚本失败: %v", taskID, err)
	}

	return nil // 如果没有错误发生，返回 nil
}

// HandleUpdateProject 处理更新项目消息
func (h *MessageHandler) HandleUpdateProject(message []byte) error {
	textMessage, taskID, err := h.parseMessage(message)
	if err != nil {
		return err
	}

	// 处理消息
	log.Printf("收到update_project消息: %v", textMessage.Data)
	// 1. 下载项目
	// 2. 解压项目
	// 3. 执行初始化脚本
	// 4. 上报启动结果

	// 如果出现错误，确保包含任务ID
	if false { // 示例条件，实际根据处理结果判断
		return h.reportError(model.UpdateProject, "示例错误", taskID, fmt.Errorf("示例错误"))
	}

	return nil // 如果没有错误发生，返回nil
}

// HandleUpdateResource 处理更新资源消息
func (h *MessageHandler) HandleUpdateResource(message []byte) error {
	textMessage, taskID, err := h.parseMessage(message)
	if err != nil {
		return err
	}

	// 处理消息
	log.Printf("收到update_resource消息: %v", textMessage.Data)
	// 1. 下载资源
	// 2. 解压资源
	// 3. 执行初始化脚本
	// 4. 上报启动结果

	// 如果出现错误，确保包含任务ID
	if false { // 示例条件，实际根据处理结果判断
		return h.reportError(model.UpdateResources, "示例错误", taskID, fmt.Errorf("示例错误"))
	}

	return nil // 如果没有错误发生，返回nil
}
